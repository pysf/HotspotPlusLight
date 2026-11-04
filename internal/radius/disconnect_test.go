package radius

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"

	"layeh.com/radius"

	"hotspotpluslight/internal/domain"
)

type coaSettings struct{ port int }

func (c coaSettings) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{RadiusSharedSecret: "s3cret", CoaPort: c.port}, nil
}

type noLink struct{}

func (noLink) Disconnect(string, string) {}

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
	d := NewDisconnector(coaSettings{port}, noLink{}, slog.Default())
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

// Wrong every time: the kick ends as "no answer" after the retries.
func TestDisconnectBadMAEveryTime(t *testing.T) {
	port, n := fakeRouter(t, func(int) bool { return true })
	if err := kick(t, port); !errors.Is(err, domain.ErrKickTimeout) {
		t.Fatalf("want ErrKickTimeout, got %v", err)
	}
	if n.Load() != attempts {
		t.Fatalf("%d requests, want %d", n.Load(), attempts)
	}
}
