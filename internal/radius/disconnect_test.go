package radius

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"layeh.com/radius"

	"hotspotpluslight/internal/domain"
)

type coaSettings struct {
	port   int
	secret string // empty means "s3cret"
}

func (c coaSettings) GetSettings(context.Context) (domain.Settings, error) {
	if c.secret == "none" {
		return domain.Settings{CoaPort: c.port}, nil
	}
	return domain.Settings{RadiusSharedSecret: "s3cret", CoaPort: c.port}, nil
}

type noLink struct{}

func (noLink) Disconnect(string, string) {}

// recLink keeps the last outcome.
type recLink struct{ outcome string }

func (r *recLink) Disconnect(_, outcome string) { r.outcome = outcome }

// fakeRouter answers each Disconnect-Request with an ACK. badMA(n) says whether
// the n-th ACK (from 1) carries a wrong Message-Authenticator. It returns the port
// and a counter of requests.
func fakeRouter(t *testing.T, badMA func(n int) bool) (int, *atomic.Int32) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var n atomic.Int32
	go func() {
		buf := make([]byte, radius.MaxPacketLength)
		for {
			m, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req, err := radius.Parse(append([]byte(nil), buf[:m]...), []byte("s3cret"))
			if err != nil {
				continue
			}
			resp := req.Response(radius.CodeDisconnectACK)
			if err := sealMA(resp, req.Authenticator); err != nil {
				return
			}
			if badMA(int(n.Add(1))) {
				resp.Attributes[0].Attribute[0] ^= 0xff
			}
			if out, err := resp.Encode(); err == nil {
				conn.WriteTo(out, from)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, &n
}

func kick(t *testing.T, port int) error {
	t.Helper()
	d := NewDisconnector(coaSettings{port: port}, noLink{}, slog.Default())
	return d.Disconnect(t.Context(), domain.SessionTarget{NASIP: "127.0.0.1", Username: "alice", AcctSessionID: "s1"})
}

// A reply with a wrong Message-Authenticator is not believed; the next good one is.
func TestDisconnectBadMAThenGood(t *testing.T) {
	port, n := fakeRouter(t, func(n int) bool { return n == 1 })
	if err := kick(t, port); err != nil {
		t.Fatalf("want ACK on the retry, got %v", err)
	}
	if n.Load() != 2 {
		t.Fatalf("%d requests, want 2", n.Load())
	}
}

// Wrong every time: the kick ends as ErrKickBadReply (not "no answer") after the
// retries, and the panel outcome says so (review B9).
func TestDisconnectBadMAEveryTime(t *testing.T) {
	port, n := fakeRouter(t, func(int) bool { return true })
	link := &recLink{}
	d := NewDisconnector(coaSettings{port: port}, link, slog.Default())
	err := d.Disconnect(t.Context(), domain.SessionTarget{NASIP: "127.0.0.1", Username: "alice", AcctSessionID: "s1"})
	if !errors.Is(err, domain.ErrKickBadReply) {
		t.Fatalf("want ErrKickBadReply, got %v", err)
	}
	if link.outcome != "bad reply (check the shared secret)" {
		t.Fatalf("outcome %q", link.outcome)
	}
	if n.Load() != attempts {
		t.Fatalf("%d requests, want %d", n.Load(), attempts)
	}
}

// An empty shared secret is a configuration error at once, with nothing sent (review B9).
func TestDisconnectEmptySecret(t *testing.T) {
	port, n := fakeRouter(t, func(int) bool { return false })
	link := &recLink{}
	d := NewDisconnector(coaSettings{port: port, secret: "none"}, link, slog.Default())
	start := time.Now()
	err := d.Disconnect(t.Context(), domain.SessionTarget{NASIP: "127.0.0.1", Username: "alice", AcctSessionID: "s1"})
	if !errors.Is(err, domain.ErrKickNoSecret) {
		t.Fatalf("want ErrKickNoSecret, got %v", err)
	}
	if time.Since(start) > attemptTimeout/2 || n.Load() != 0 {
		t.Fatalf("took %v, %d requests sent", time.Since(start), n.Load())
	}
	if link.outcome != "shared secret not set" {
		t.Fatalf("outcome %q", link.outcome)
	}
}
