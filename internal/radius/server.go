// Package radius is the UDP RADIUS server and the Disconnect client. PAP
// Access-Requests are answered by an auth.Evaluator, and accounting packets are
// applied by the session service.
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
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/mikrotik"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// SecretProvider returns the current shared secret. It is called per packet, so
// a settings change applies without a restart (PLAN 7.1).
type SecretProvider interface {
	RadiusSecret(ctx context.Context) (string, error)
}

// secretSource adapts a SecretProvider to the library's SecretSource.
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

// LinkRecorder receives the router link events (PLAN 8.2, linkstatus.Recorder).
// Addresses are "ip:port".
type LinkRecorder interface {
	AccessRequest(addr string)
	Accounting(addr string)
	BadSecret(addr string)
}

// FailureRecorder counts one failed login by reason (BR-27, implemented by dashboard.Service).
type FailureRecorder interface {
	RecordLoginFailure(ctx context.Context, reason domain.Reason) error
}

// Option changes how New builds a Server.
type Option func(*Server)

// WithFailureRecorder makes the server count every Access-Reject it sends (BR-27).
func WithFailureRecorder(f FailureRecorder) Option { return func(s *Server) { s.failures = f } }

// WithClock sets the clock of the retransmission cache. The default is the system clock.
func WithClock(c clock.Clock) Option { return func(s *Server) { s.clock = c } }

// Server runs the auth and accounting packet servers.
//
// Shutdown does not use radius.PacketServer.Shutdown. It has a race in
// layeh.com/radius@v0.0.0-20231213012653: Serve registers its listener and only
// later calls activeAdd(), so a Shutdown in that gap closes lastActive twice
// ("close of closed channel"). Server stops the read loops itself
// (lateCloseConn) and counts handlers in flight with its own WaitGroup.
type Server struct {
	log     *slog.Logger
	secrets SecretProvider
	login   Authenticator
	acctg   Accountant
	link    LinkRecorder
	clock   clock.Clock
	// failures counts failed logins (nil: not counted). retries makes a retransmitted reject count once.
	failures FailureRecorder
	retries  *retryCache
	auth     *radius.PacketServer
	acct     *radius.PacketServer

	mu       sync.Mutex
	stopping bool
	inflight sync.WaitGroup // handlers in flight
	sniffSem chan struct{}  // bounds the secret checks that run at once
	loops    sync.WaitGroup // Serve loops
	conns    []net.PacketConn
	stoppers []*lateCloseConn
}

// New returns a Server. Call Serve to start it.
func New(secrets SecretProvider, login Authenticator, acctg Accountant, link LinkRecorder, log *slog.Logger, opts ...Option) *Server {
	src := secretSource{secrets}
	s := &Server{log: log, secrets: secrets, login: login, acctg: acctg, link: link, clock: clock.Real{}, retries: newRetryCache(), sniffSem: make(chan struct{}, maxSniffs)}
	for _, o := range opts {
		o(s)
	}
	s.auth = &radius.PacketServer{SecretSource: src, Handler: s.track(s.handleAuth)}
	s.acct = &radius.PacketServer{SecretSource: src, Handler: s.track(s.handleAcct)}
	return s
}

// track counts h as in flight. After Shutdown began, new packets are dropped
// and the router retransmits.
func (s *Server) track(h radius.HandlerFunc) radius.Handler {
	return radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		s.inflight.Add(1)
		s.mu.Unlock()
		defer s.inflight.Done()
		if s.log.Enabled(r.Context(), slog.LevelDebug) {
			s.log.Debug("radius request", "from", r.RemoteAddr.String(), "to", r.LocalAddr.String(), "code", r.Code.String(), "id", r.Identifier)
		}
		h(&loggedWriter{ResponseWriter: w, log: s.log, to: r.RemoteAddr}, r)
	})
}

// loggedWriter logs each reply at debug level, and a failed write as an error.
// Without it a reply that never leaves the host leaves no trace.
type loggedWriter struct {
	radius.ResponseWriter
	log *slog.Logger
	to  net.Addr
}

func (w *loggedWriter) Write(p *radius.Packet) error {
	switch p.Code {
	case radius.CodeAccessAccept, radius.CodeAccessReject, radius.CodeAccessChallenge:
		// This relies on p.Authenticator still being the request's authenticator, which
		// holds for packets made by request.Response(). Encode then adds the Response
		// Authenticator over the finished packet, the Message-Authenticator included.
		if err := sealMA(p, p.Authenticator); err != nil {
			w.log.Error("radius reply not sent", "to", w.to.String(), "code", p.Code.String(), "err", err)
			return err
		}
	}
	err := w.ResponseWriter.Write(p)
	if err != nil {
		w.log.Error("radius reply not sent", "to", w.to.String(), "code", p.Code.String(), "err", err)
		return err
	}
	if w.log.Enabled(context.Background(), slog.LevelDebug) {
		w.log.Debug("radius reply sent", "to", w.to.String(), "code", p.Code.String(), "id", p.Identifier)
	}
	return nil
}

// Serve starts the auth and accounting listeners on the given connections. It
// returns at once; Shutdown stops them.
func (s *Server) Serve(authConn, acctConn net.PacketConn) {
	a := &lateCloseConn{PacketConn: authConn}
	b := &lateCloseConn{PacketConn: acctConn, onRead: s.sniffAcct}
	s.mu.Lock()
	s.conns = []net.PacketConn{authConn, acctConn}
	s.stoppers = []*lateCloseConn{a, b}
	s.loops.Add(2)
	s.mu.Unlock()
	go s.run("auth", s.auth, a)
	go s.run("acct", s.acct, b)
}

// lateCloseConn lets requests in flight at shutdown still be answered. stop ends
// the reading only: ReadFrom then returns net.ErrClosed, which the library treats
// as fatal, not temporary. Server.Shutdown closes the socket once every handler
// has finished, so Close is a no-op. onRead, if set, sees every datagram before
// the library does.
type lateCloseConn struct {
	net.PacketConn
	onRead  func(pkt []byte, from net.Addr)
	stopped atomic.Bool
}

func (c *lateCloseConn) Close() error { return nil }

func (c *lateCloseConn) stop() {
	c.stopped.Store(true)
	c.PacketConn.SetReadDeadline(time.Unix(1, 0))
}

func (c *lateCloseConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.stopped.Load() {
		return 0, nil, net.ErrClosed
	}
	n, addr, err := c.PacketConn.ReadFrom(b)
	if c.stopped.Load() {
		return 0, nil, net.ErrClosed
	}
	if err == nil && c.onRead != nil {
		c.onRead(b[:n], addr)
	}
	return n, addr, err
}

// maxSniffs is how many wrong-secret checks may run at once.
const maxSniffs = 64

// sniffAcct sees accounting datagrams before the library does. The library drops
// a packet with a bad Request Authenticator without telling us who sent it; this
// is how the link status panel learns about a wrong shared secret. The check runs
// in its own goroutine, so a busy database never stalls the UDP read loop.
func (s *Server) sniffAcct(pkt []byte, from net.Addr) {
	if len(pkt) < 20 || radius.Code(pkt[0]) != radius.CodeAccountingRequest {
		return
	}
	select {
	case s.sniffSem <- struct{}{}:
	default:
		return // a missed check only means a missed wrong-secret alarm; a flood must not pile up goroutines
	}
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		<-s.sniffSem
		return
	}
	s.inflight.Add(1)
	s.mu.Unlock()
	pkt = bytes.Clone(pkt) // the read loop reuses its buffer
	go func() {
		defer s.inflight.Done()
		defer func() { <-s.sniffSem }()
		s.checkAcctSecret(pkt, from)
	}()
}

// checkAcctSecret repeats the library's authenticator check to find a wrong secret.
func (s *Server) checkAcctSecret(pkt []byte, from net.Addr) {
	secret, err := s.secrets.RadiusSecret(context.Background())
	if err != nil || secret == "" || radius.IsAuthenticRequest(pkt, []byte(secret)) {
		return
	}
	s.link.BadSecret(from.String())
	s.log.Warn("Accounting-Request dropped: its authenticator does not verify, probably a wrong shared secret", "from", from.String())
}

func (s *Server) run(name string, ps *radius.PacketServer, c *lateCloseConn) {
	defer s.loops.Done()
	if err := ps.Serve(c); err != nil && !c.stopped.Load() {
		s.log.Error("radius listener stopped", "listener", name, "err", err)
	}
}

// Shutdown stops reading, waits for the handlers in flight (their replies still
// go out) until ctx ends, and then closes the sockets. It returns ctx.Err() if
// the wait timed out.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.stopping = true
	stoppers, conns := s.stoppers, s.conns
	s.mu.Unlock()
	for _, c := range stoppers {
		c.stop()
	}
	done := make(chan struct{})
	go func() {
		s.loops.Wait()
		s.inflight.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = ctx.Err()
	}
	for _, c := range conns {
		c.Close()
	}
	return err
}

// handleAuth answers Access-Requests (PAP only, PLAN 7.2). It sends no reply
// when it cannot give a trustworthy answer: a probable wrong shared secret, a
// malformed packet, or an infrastructure failure.
func (s *Server) handleAuth(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccessRequest {
		return // other codes are dropped (PLAN 7.1)
	}
	from := r.RemoteAddr.String()
	ctx := context.WithoutCancel(r.Context()) // a shutdown lets requests in flight finish
	// A router that sends Message-Authenticator must send a correct one. Without one
	// the request is accepted (see messageauth.go).
	maPresent, maOK := checkMA(r.Packet, r.Packet.Authenticator)
	if maPresent && !maOK {
		// RouterOS 7.15+ always sends Message-Authenticator, so this is how a wrong secret shows up.
		s.link.BadSecret(from) // MONITOR: spoofable on UDP; the owner accepts a false alarm
		s.log.Warn("Access-Request dropped: its Message-Authenticator does not verify, probably a wrong shared secret", "from", from)
		return
	}
	if isCHAP(r.Packet) {
		s.link.AccessRequest(from)
		// The warning is how the admin notices a router that is not on PAP (D4, PLAN 13.1).
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
	case !(maPresent && maOK) && (!plausiblePassword(raw) || !zeroPadded(r.Packet)):
		// layeh does not authenticate Access-Requests, so a wrong secret shows up only
		// as a User-Password that decrypts to noise. A verified Message-Authenticator
		// already proves the secret, so the heuristics are skipped then. Never answer it.
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

// reply writes the Access-Accept or Access-Reject for v, and logs a reject.
func (s *Server) reply(w radius.ResponseWriter, r *radius.Request, v auth.Verdict) {
	var resp *radius.Packet
	if v.Accepted() {
		resp = r.Response(radius.CodeAccessAccept)
		g := v.Grant
		rfc2869.AcctInterimInterval_Set(resp, rfc2869.AcctInterimInterval(g.InterimSeconds))
		rfc2865.SessionTimeout_Set(resp, rfc2865.SessionTimeout(g.SessionTimeout))
		// Layer 1 of the enforcement (O3): the exact bytes left, in both directions,
		// as low 32 bits plus -Gigawords.
		// The type ids are in PLAN 7.5. The same value goes in both directions, so the
		// server checks the sum.
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
		// The Reply-Message is only the code; the log keeps the English text too (O20).
		name, _ := rfc2865.UserName_LookupString(r.Packet)
		s.log.Info("access-reject", "username", name, "code", string(v.Reason), "message", v.Reason.Message())
		rfc2865.ReplyMessage_SetString(resp, v.Reason.ReplyMessage())
		s.countFailure(r, v.Reason)
	}
	_ = w.Write(resp) // loggedWriter logs a failure
}

// countFailure adds a reject to the daily failed-login count (BR-27), once per request: the router
// repeats a request every few seconds when it does not get the reply. A counter error is logged and
// never stops the reject. The request is forgotten again then, so its next retransmission is counted.
// MONITOR: the cache is per process, so a restart can count one repeated request twice.
// MONITOR: the count is written before the reply leaves, one SQLite write per reject. That is fine at LAN scale.
// MONITOR: above 1024 distinct rejects per minute the oldest requests are evicted and their repeats count twice.
// MONITOR: the key has the source port, so a router that retries from a new port is counted again.
func (s *Server) countFailure(r *radius.Request, reason domain.Reason) {
	if s.failures == nil {
		return
	}
	key := retryKey{from: r.RemoteAddr.String(), id: r.Identifier, auth: r.Authenticator}
	if s.retries.repeated(key, s.clock.Now()) {
		return
	}
	if err := s.failures.RecordLoginFailure(context.WithoutCancel(r.Context()), reason); err != nil {
		s.retries.forget(key)
		s.log.Error("failed-login counter not written; the reject is still sent", "reason", string(reason), "err", err)
	}
}

// microsoftVendorID is the vendor id of the MS-CHAP attributes.
const microsoftVendorID = 311

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

// zeroPadded reports whether the decrypted User-Password is padded with zero
// bytes only (RFC 2865 5.2). The library cuts the plaintext at the first zero
// byte, so a wrong-secret block with an early zero byte would look like a short
// valid password (about 1 in 150 packets).
// RouterOS pads with zeros.
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

// plausiblePassword reports whether b is text a person could have typed: valid
// UTF-8 without control characters. A wrong secret gives random bytes, which
// almost never pass.
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

// handleAcct applies an Accounting-Request and answers it only after the change
// is committed (PLAN 7.3). Without a reply the router retransmits. The library
// has already verified the Request Authenticator (E4.12).
func (s *Server) handleAcct(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccountingRequest {
		return
	}
	from := r.RemoteAddr.String()
	s.link.Accounting(from)
	// Shutdown cancels r.Context(), but a transaction in flight must still commit and be answered (E8.06).
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
	_ = w.Write(r.Response(radius.CodeAccountingResponse)) // loggedWriter logs a failure
}

// record reads the attributes of PLAN 7.3 into rec. An absent attribute is
// zero or empty; a malformed one is an error, so the packet is dropped, not counted as 0.
func (s *Server) record(r *radius.Request, rec domain.AcctRecord, sid string) (domain.AcctRecord, error) {
	p := r.Packet
	rec.SessionID = sid
	host, _, err := net.SplitHostPort(r.RemoteAddr.String())
	if err != nil {
		host = r.RemoteAddr.String()
	}
	rec.NASIP = host // the packet's source address is what a Disconnect is sent to (BR-18)
	var errs []error
	// opt reports whether the attribute is present; a missing one is fine, any other error is collected.
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
