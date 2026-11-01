package radius

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"layeh.com/radius"
)

type staticSecret string

func (s staticSecret) RadiusSecret(context.Context) (string, error) { return string(s), nil }

func TestRejectsAccessRequest(t *testing.T) {
	ac, _ := net.ListenPacket("udp", "127.0.0.1:0")
	cc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s := New(staticSecret("s3cret"), slog.Default())
	s.Serve(ac, cc)
	defer s.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reply, err := radius.Exchange(ctx, radius.New(radius.CodeAccessRequest, []byte("s3cret")), ac.LocalAddr().String())
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
}
