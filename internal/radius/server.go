// Package radius is the UDP RADIUS server: PAP Access-Requests are answered by
// the auth.Evaluator (Phase 3); accounting packets are applied by the session
// service (Phase 4).
package radius

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"unicode/utf8"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

// SecretProvider returns the current shared secret. It is called per packet so
// settings changes apply without restart (PLAN 7.1).
type SecretProvider interface {
	RadiusSecret(ctx context.Context) (string, error)
}

type secretSource struct{ p SecretProvider }

func (s secretSource) RADIUSSecret(ctx context.Context, _ net.Addr) ([]byte, error) {
	v, err := s.p.RadiusSecret(ctx)
	return []byte(v), err
}

// Authenticator is the member login check (implemented by auth.Evaluator).
type Authenticator interface {
	Login(ctx context.Context, username, password string) (auth.Verdict, error)
}

// Accountant applies one accounting record (implemented by session.Service).
// It returns nil only after the change is committed.
type Accountant interface {
	Account(ctx context.Context, rec domain.AcctRecord) error
}

// Server owns the auth and accounting packet servers.
type Server struct {
	log   *slog.Logger
	login Authenticator
	acctg Accountant
	auth  *radius.PacketServer
	acct  *radius.PacketServer
}

func New(secrets SecretProvider, login Authenticator, acctg Accountant, log *slog.Logger) *Server {
	src := secretSource{secrets}
	s := &Server{log: log, login: login, acctg: acctg}
	s.auth = &radius.PacketServer{SecretSource: src, Handler: radius.HandlerFunc(s.handleAuth)}
	s.acct = &radius.PacketServer{SecretSource: src, Handler: radius.HandlerFunc(s.handleAcct)}
	return s
}

// Serve runs both listeners on the given connections until Shutdown.
func (s *Server) Serve(authConn, acctConn net.PacketConn) {
	go s.run("auth", s.auth, authConn)
	go s.run("acct", s.acct, acctConn)
}

func (s *Server) run(name string, ps *radius.PacketServer, c net.PacketConn) {
	if err := ps.Serve(c); err != nil && err != radius.ErrServerShutdown {
		s.log.Error("radius listener stopped", "listener", name, "err", err)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	e1 := s.auth.Shutdown(ctx)
	e2 := s.acct.Shutdown(ctx)
	if e1 != nil {
		return e1
	}
	return e2
}

// handleAuth answers Access-Requests (PAP only, PLAN 7.2). It stays silent
// (no reply) when it cannot give a trustworthy answer: a probably wrong shared
// secret, a malformed packet, or an infrastructure failure.
func (s *Server) handleAuth(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccessRequest {
		return // other codes are dropped (PLAN 7.1)
	}
	from := r.RemoteAddr.String()
	if isCHAP(r.Packet) {
		// This is how we notice a router that is not on PAP (D4, PLAN 13.1).
		s.log.Warn("CHAP/MS-CHAP Access-Request rejected: only PAP is supported", "from", from)
		s.reply(w, r, auth.Verdict{Reason: domain.ReasonUnsupportedAuth})
		return
	}
	username, _ := rfc2865.UserName_LookupString(r.Packet)
	raw, err := rfc2865.UserPassword_Lookup(r.Packet)
	switch {
	case errors.Is(err, radius.ErrNoAttribute):
		s.reply(w, r, auth.Verdict{Reason: domain.ReasonInvalidCredentials})
		return
	case err != nil:
		s.log.Warn("Access-Request with a malformed User-Password dropped", "from", from, "err", err)
		return
	case !plausiblePassword(raw):
		// layeh does not authenticate Access-Requests, so a wrong shared secret
		// shows up only as a User-Password that decrypts to noise. Never answer it.
		s.log.Warn("Access-Request dropped: User-Password does not decrypt, probably a wrong shared secret", "from", from)
		return
	}
	v, err := s.login.Login(r.Context(), username, string(raw))
	if err != nil {
		s.log.Error("login check failed; no reply", "from", from, "err", err)
		return
	}
	if v.Grant.XmitClamped {
		s.log.Warn("Mikrotik-Xmit-Limit clamped to the 32-bit maximum (the remaining data exceeds 4294967295 bytes)", "username", username)
	}
	s.reply(w, r, v)
}

// reply writes the Access-Accept or Access-Reject for v.
func (s *Server) reply(w radius.ResponseWriter, r *radius.Request, v auth.Verdict) {
	var resp *radius.Packet
	if v.Accepted() {
		resp = r.Response(radius.CodeAccessAccept)
		g := v.Grant
		rfc2869.AcctInterimInterval_Set(resp, rfc2869.AcctInterimInterval(g.InterimSeconds))
		rfc2865.SessionTimeout_Set(resp, rfc2865.SessionTimeout(g.SessionTimeout))
		// VERIFY-ON-HARDWARE (PLAN 13.2): the vendor-14988 type ids (Xmit-Limit 2,
		// Rate-Limit 8) come from the MikroTik dictionary in layeh.com/radius and
		// match PLAN 7.5; the semantics (download-only cap, the 4294967295 clamp)
		// and the accepted Rate-Limit syntax still need a real router.
		mikrotik.MikrotikXmitLimit_Set(resp, mikrotik.MikrotikXmitLimit(g.XmitLimit))
		if g.RateLimit != "" {
			mikrotik.MikrotikRateLimit_SetString(resp, g.RateLimit)
		}
	} else {
		resp = r.Response(radius.CodeAccessReject)
		rfc2865.ReplyMessage_SetString(resp, v.Reason.ReplyMessage())
	}
	if err := w.Write(resp); err != nil {
		s.log.Error("write reply", "err", err)
	}
}

const (
	microsoftVendorID = 311 // MS-CHAP attributes are vendor-specific
)

// isCHAP reports a CHAP-Password or any Microsoft vendor attribute (MS-CHAP v1/v2).
func isCHAP(p *radius.Packet) bool {
	if _, ok := p.Lookup(rfc2865.CHAPPassword_Type); ok {
		return true
	}
	for _, a := range p.Attributes {
		if a.Type != rfc2865.VendorSpecific_Type {
			continue
		}
		if id, _, err := radius.VendorSpecific(a.Attribute); err == nil && id == microsoftVendorID {
			return true
		}
	}
	return false
}

// plausiblePassword is true for text a person could have typed: valid UTF-8
// without control characters. A decryption with the wrong secret yields random
// bytes, which fail this with overwhelming probability.
func plausiblePassword(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// handleAcct applies an Accounting-Request and answers it only after the
// transaction has committed (PLAN 7.3). The library has already verified the
// Request Authenticator with the shared secret (bad packets never get here, E4.12).
// No reply is sent when the change could not be stored: the router retransmits.
func (s *Server) handleAcct(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccountingRequest {
		return
	}
	from := r.RemoteAddr.String()
	status, err := rfc2866.AcctStatusType_Lookup(r.Packet)
	if err != nil {
		s.log.Warn("Accounting-Request without Acct-Status-Type dropped", "from", from)
		return
	}
	rec := domain.AcctRecord{Status: domain.AcctStatus(status)}
	if rec.Status == domain.AcctStart || rec.Status == domain.AcctInterim || rec.Status == domain.AcctStop {
		sid, err := rfc2866.AcctSessionID_LookupString(r.Packet)
		if err != nil || sid == "" {
			s.log.Warn("Accounting-Request without Acct-Session-Id dropped", "from", from)
			return
		}
		rec, err = s.record(r, rec, sid)
		if err != nil {
			s.log.Warn("Accounting-Request with a malformed attribute dropped", "from", from, "session", sid, "err", err)
			return
		}
		if err := s.acctg.Account(r.Context(), rec); err != nil {
			s.log.Error("accounting failed; no reply", "from", from, "err", err)
			return
		}
	}
	// Accounting-On/Off and every other status type are acknowledged and change nothing.
	// MONITOR: see session.Service.Account.
	if err := w.Write(r.Response(radius.CodeAccountingResponse)); err != nil {
		s.log.Error("write reply", "err", err)
	}
}

// record reads the attributes of PLAN 7.3. An absent attribute is 0/empty; a
// present but malformed one is an error (the packet is dropped, not counted as 0).
func (s *Server) record(r *radius.Request, rec domain.AcctRecord, sid string) (domain.AcctRecord, error) {
	p := r.Packet
	rec.SessionID = sid
	host, _, err := net.SplitHostPort(r.RemoteAddr.String())
	if err != nil {
		host = r.RemoteAddr.String()
	}
	rec.NASIP = host // the packet's source address is what a Disconnect is sent to (BR-18)
	var errs []error
	// opt returns the lookup result, or the zero value when the attribute is absent.
	opt := func(name string, err error) bool {
		if err == nil {
			return true
		}
		if !errors.Is(err, radius.ErrNoAttribute) {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		return false
	}
	var e error
	rec.Username, e = rfc2865.UserName_LookupString(p)
	opt("User-Name", e)
	rec.NASIdentifier, e = rfc2865.NASIdentifier_LookupString(p)
	opt("NAS-Identifier", e)
	ip, e := rfc2865.FramedIPAddress_Lookup(p)
	if opt("Framed-IP-Address", e) && ip != nil {
		rec.FramedIP = ip.String()
	}
	rec.MAC, e = rfc2865.CallingStationID_LookupString(p)
	opt("Calling-Station-Id", e)
	c, e := rfc2866.AcctTerminateCause_Lookup(p)
	if opt("Acct-Terminate-Cause", e) {
		rec.TerminateCause = c.String()
	}
	inO, e := rfc2866.AcctInputOctets_Lookup(p)
	opt("Acct-Input-Octets", e)
	outO, e := rfc2866.AcctOutputOctets_Lookup(p)
	opt("Acct-Output-Octets", e)
	inG, e := rfc2869.AcctInputGigawords_Lookup(p)
	opt("Acct-Input-Gigawords", e)
	outG, e := rfc2869.AcctOutputGigawords_Lookup(p)
	opt("Acct-Output-Gigawords", e)
	secs, e := rfc2866.AcctSessionTime_Lookup(p)
	opt("Acct-Session-Time", e)
	if len(errs) > 0 {
		return rec, errors.Join(errs...)
	}
	rec.Cum = domain.CumulativeFromAcct(uint32(inO), uint32(inG), uint32(outO), uint32(outG), uint32(secs))
	return rec, nil
}
