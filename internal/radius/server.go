// Package radius is the UDP RADIUS server: PAP Access-Requests are answered by
// the auth.Evaluator (Phase 3); accounting packets are applied by the session
// service (Phase 4).
package radius

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"time"
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

// LinkRecorder receives the router link events (PLAN 8.2; implemented by
// linkstatus.Recorder). Source addresses are "ip:port".
type LinkRecorder interface {
	AccessRequest(addr string)
	Accounting(addr string)
	BadSecret(addr string)
}

// Server owns the auth and accounting packet servers.
type Server struct {
	log     *slog.Logger
	secrets SecretProvider
	login   Authenticator
	acctg   Accountant
	link    LinkRecorder
	auth    *radius.PacketServer
	acct    *radius.PacketServer
	conns   []net.PacketConn
}

func New(secrets SecretProvider, login Authenticator, acctg Accountant, link LinkRecorder, log *slog.Logger) *Server {
	src := secretSource{secrets}
	s := &Server{log: log, secrets: secrets, login: login, acctg: acctg, link: link}
	s.auth = &radius.PacketServer{SecretSource: src, Handler: radius.HandlerFunc(s.handleAuth)}
	s.acct = &radius.PacketServer{SecretSource: src, Handler: radius.HandlerFunc(s.handleAcct)}
	return s
}

// Serve runs both listeners on the given connections until Shutdown.
func (s *Server) Serve(authConn, acctConn net.PacketConn) {
	s.conns = []net.PacketConn{authConn, acctConn}
	go s.run("auth", s.auth, &lateCloseConn{PacketConn: authConn})
	go s.run("acct", s.acct, &lateCloseConn{PacketConn: acctConn, onRead: s.sniffAcct})
}

// lateCloseConn lets requests in flight at shutdown still be answered. The
// library closes its listeners the moment Shutdown starts and only then waits
// for the running handlers, so their replies would fail. Close here only stops
// the reading (an immediate read deadline); the socket is really closed by
// Server.Shutdown once every handler has finished. The library calls Close only
// from its own Shutdown, after it set its shutdown flag, so a deadline error is
// always read as "shutting down" and never retried as a temporary error; and
// Server.Shutdown closes the real sockets even if a Serve loop ended early. onRead, if set, sees every
// datagram before the library does.
type lateCloseConn struct {
	net.PacketConn
	onRead func(pkt []byte, from net.Addr)
}

func (c *lateCloseConn) Close() error { return c.PacketConn.SetReadDeadline(time.Unix(1, 0)) }

func (c *lateCloseConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err == nil && c.onRead != nil {
		c.onRead(b[:n], addr)
	}
	return n, addr, err
}

// sniffAcct looks at accounting datagrams before the library does. The library
// drops a packet whose Request Authenticator does not verify without telling us
// who sent it; this is how the link status panel learns about wrong shared
// secrets (it repeats the library's own check).
//
// MONITOR: this runs in the UDP read loop and does one SQLite read (the current
// secret, so settings changes apply at once) plus one MD5 per accounting packet.
// Fine at LAN scale (one router, an interim every 60 s per user); if accounting
// volume ever grows, cache the secret in memory and invalidate it on settings save.
func (s *Server) sniffAcct(pkt []byte, from net.Addr) {
	if len(pkt) >= 20 && radius.Code(pkt[0]) == radius.CodeAccountingRequest {
		s.checkAcctSecret(pkt, from)
	}
}

func (s *Server) checkAcctSecret(pkt []byte, from net.Addr) {
	secret, err := s.secrets.RadiusSecret(context.Background())
	if err != nil || secret == "" || radius.IsAuthenticRequest(pkt, []byte(secret)) {
		return
	}
	s.link.BadSecret(from.String())
	s.log.Warn("Accounting-Request dropped: its authenticator does not verify, probably a wrong shared secret", "from", from.String())
}

func (s *Server) run(name string, ps *radius.PacketServer, c net.PacketConn) {
	if err := ps.Serve(c); err != nil && err != radius.ErrServerShutdown {
		s.log.Error("radius listener stopped", "listener", name, "err", err)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Waits for the handlers in flight; their replies still go out (lateCloseConn). Then the sockets are released.
	err := errors.Join(s.auth.Shutdown(ctx), s.acct.Shutdown(ctx))
	for _, c := range s.conns {
		c.Close()
	}
	return err
}

// handleAuth answers Access-Requests (PAP only, PLAN 7.2). It stays silent
// (no reply) when it cannot give a trustworthy answer: a probably wrong shared
// secret, a malformed packet, or an infrastructure failure.
func (s *Server) handleAuth(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccessRequest {
		return // other codes are dropped (PLAN 7.1)
	}
	from := r.RemoteAddr.String()
	ctx := context.WithoutCancel(r.Context()) // a shutdown lets requests in flight finish
	if isCHAP(r.Packet) {
		s.link.AccessRequest(from)
		// This is how we notice a router that is not on PAP (D4, PLAN 13.1).
		s.log.Warn("CHAP/MS-CHAP Access-Request rejected: only PAP is supported", "from", from)
		s.reply(w, r, auth.Verdict{Reason: domain.ReasonUnsupportedAuth})
		return
	}
	username, _ := rfc2865.UserName_LookupString(r.Packet)
	raw, err := rfc2865.UserPassword_Lookup(r.Packet)
	switch {
	case errors.Is(err, radius.ErrNoAttribute):
		s.link.AccessRequest(from)
		s.reply(w, r, auth.Verdict{Reason: domain.ReasonInvalidCredentials})
		return
	case err != nil:
		s.link.AccessRequest(from)
		s.log.Warn("Access-Request with a malformed User-Password dropped", "from", from, "err", err)
		return
	case !plausiblePassword(raw) || !zeroPadded(r.Packet):
		// layeh does not authenticate Access-Requests, so a wrong shared secret
		// shows up only as a User-Password that decrypts to noise. Never answer it.
		s.link.BadSecret(from) // MONITOR: spoofable on UDP, see internal/linkstatus
		s.log.Warn("Access-Request dropped: User-Password does not decrypt, probably a wrong shared secret", "from", from)
		return
	}
	s.link.AccessRequest(from)
	v, err := s.login.Login(ctx, username, string(raw))
	if err != nil {
		s.log.Error("login check failed; no reply", "from", from, "err", err)
		return
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
		// Layer 1 of the enforcement (O3, 2026-11-04): the router gets the exact bytes
		// left, in both directions, split into the low 32 bits and -Gigawords.
		// VERIFY-ON-HARDWARE (PLAN 13.2): the type ids (Recv-Limit 1, Xmit-Limit 2,
		// Recv-Limit-Gigawords 14, Xmit-Limit-Gigawords 15, Rate-Limit 8) come from the
		// MikroTik dictionary in layeh.com/radius and match PLAN 7.5. Still to prove on a
		// router: that Recv/Xmit mean upload/download in the Hotspot (the MikroTik doc does
		// not define the direction), that the Gigawords limits are honoured, and the
		// Rate-Limit syntax.
		if g.DataLimited {
			mikrotik.MikrotikRecvLimit_Set(resp, mikrotik.MikrotikRecvLimit(g.DataLow))
			mikrotik.MikrotikXmitLimit_Set(resp, mikrotik.MikrotikXmitLimit(g.DataLow))
			if g.DataGiga > 0 {
				mikrotik.MikrotikRecvLimitGigawords_Set(resp, mikrotik.MikrotikRecvLimitGigawords(g.DataGiga))
				mikrotik.MikrotikXmitLimitGigawords_Set(resp, mikrotik.MikrotikXmitLimitGigawords(g.DataGiga))
			}
		}
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

// zeroPadded checks the padding of the decrypted User-Password. A real router pads
// with zero bytes only. The library cuts the plaintext at the first zero byte and
// hides what follows, so a wrong-secret block of random bytes with an early zero
// byte would look like a short, valid password (about 1 in 150 packets).
// RFC 2865 §5.2 requires zero padding. VERIFY-ON-HARDWARE: confirm RouterOS pads with zeros.
func zeroPadded(p *radius.Packet) bool {
	a, ok := p.Lookup(rfc2865.UserPassword_Type)
	if !ok || len(a) < 16 || len(a)%16 != 0 || len(p.Secret) == 0 {
		return false
	}
	dec := make([]byte, 0, len(a))
	prev := p.Authenticator[:]
	for i := 0; i < len(a); i += 16 {
		h := md5.Sum(append(append([]byte{}, p.Secret...), prev...))
		for j := 0; j < 16; j++ {
			dec = append(dec, h[j]^a[i+j])
		}
		prev = a[i : i+16]
	}
	i := bytes.IndexByte(dec, 0)
	return i < 0 || !slices.ContainsFunc(dec[i:], func(b byte) bool { return b != 0 })
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
	s.link.Accounting(from)
	// Shutdown cancels r.Context(); a transaction in flight must still commit and be answered (E8.06).
	ctx := context.WithoutCancel(r.Context())
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
		if err := s.acctg.Account(ctx, rec); err != nil {
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
