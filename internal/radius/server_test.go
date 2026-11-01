package radius

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

type staticSecret string

func (s staticSecret) RadiusSecret(context.Context) (string, error) { return string(s), nil }

// fakeAuth is a hand-written Authenticator.
type fakeAuth struct {
	mu      sync.Mutex
	verdict auth.Verdict
	err     error
	gotUser string
	gotPW   string
}

func (f *fakeAuth) Login(_ context.Context, u, p string) (auth.Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotUser, f.gotPW = u, p
	return f.verdict, f.err
}

func start(t *testing.T, a Authenticator) string {
	t.Helper()
	ac, _ := net.ListenPacket("udp", "127.0.0.1:0")
	cc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s := New(staticSecret("s3cret"), a, slog.Default())
	s.Serve(ac, cc)
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return ac.LocalAddr().String()
}

func exchange(t *testing.T, addr string, p *radius.Packet, wait time.Duration) (*radius.Packet, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return radius.Exchange(ctx, p, addr)
}

func pap(user, pw string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, []byte("s3cret"))
	rfc2865.UserName_SetString(p, user)
	rfc2865.UserPassword_SetString(p, pw)
	return p
}

func TestRejectCarriesCodeAndMessage(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonOutOfBulk}}
	reply, err := exchange(t, start(t, fa), pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
	if got := rfc2865.ReplyMessage_GetString(reply); got != "OUT_OF_BULK: Your data allowance is used up." {
		t.Fatalf("reply message %q", got)
	}
	fa.mu.Lock()
	defer fa.mu.Unlock()
	if fa.gotUser != "alice" || fa.gotPW != "pw" {
		t.Fatalf("authenticator got %q/%q", fa.gotUser, fa.gotPW)
	}
}

func TestAcceptAttributes(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Grant: domain.Grant{InterimSeconds: 60, SessionTimeout: 99, XmitLimit: 7, RateLimit: "1k/1k"}}}
	reply, err := exchange(t, start(t, fa), pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessAccept {
		t.Fatalf("%v %v", reply, err)
	}
	if rfc2865.SessionTimeout_Get(reply) != 99 {
		t.Fatalf("session timeout %d", rfc2865.SessionTimeout_Get(reply))
	}
}

func TestInfrastructureErrorMeansNoReply(t *testing.T) {
	addr := start(t, &fakeAuth{err: errors.New("database is down")})
	if reply, err := exchange(t, addr, pap("alice", "pw"), 500*time.Millisecond); err == nil {
		t.Fatalf("got a reply: %v", reply.Code)
	}
}

func TestPlausiblePassword(t *testing.T) {
	for in, want := range map[string]bool{"": true, "abc123": true, "pässwörd": true, "bad\x01": false, "\xff\xfe": false, "tab\t": false} {
		if got := plausiblePassword([]byte(in)); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
}
