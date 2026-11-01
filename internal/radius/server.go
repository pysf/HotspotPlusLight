// Package radius is the UDP RADIUS server. Phase 0: every Access-Request gets a
// well-formed Reject (PLAN 11, E0.04); the PAP evaluator is Phase 3 and
// accounting is Phase 4.
package radius

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
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

// Server owns the auth and accounting packet servers.
type Server struct {
	log  *slog.Logger
	auth *radius.PacketServer
	acct *radius.PacketServer
}

func New(secrets SecretProvider, log *slog.Logger) *Server {
	src := secretSource{secrets}
	s := &Server{log: log}
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

// handleAuth: Phase 0 placeholder. MONITOR: no per-username brute-force
// protection at the RADIUS edge (PLAN 9.5, accepted by the owner).
func (s *Server) handleAuth(w radius.ResponseWriter, r *radius.Request) {
	if r.Code != radius.CodeAccessRequest {
		return // other codes are dropped (PLAN 7.1)
	}
	resp := r.Response(radius.CodeAccessReject)
	rfc2865.ReplyMessage_SetString(resp, fmt.Sprintf("%s: %s", "INVALID_CREDENTIALS", "Wrong username or password."))
	if err := w.Write(resp); err != nil {
		s.log.Error("write reply", "err", err)
	}
}

// handleAcct: Phase 4. Until then accounting packets are dropped, not answered,
// so nothing is acknowledged that was not stored.
func (s *Server) handleAcct(radius.ResponseWriter, *radius.Request) {}
