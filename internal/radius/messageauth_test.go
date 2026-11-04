package radius

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"log/slog"
	"net"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

// refMA is the reference calculation, written out byte by byte (RFC 3579 3.2):
// HMAC-MD5(secret, packet) with the Authenticator field replaced by auth and the
// 16 value bytes of the first Message-Authenticator set to zero.
func refMA(t *testing.T, wire []byte, auth []byte) []byte {
	t.Helper()
	b := append([]byte(nil), wire...)
	copy(b[4:20], auth)
	for i := 20; i+2 <= len(b); i += int(b[i+1]) {
		if b[i] == 80 {
			for j := 0; j < 16; j++ {
				b[i+2+j] = 0
			}
			break
		}
	}
	h := hmac.New(md5.New, []byte("s3cret"))
	h.Write(b)
	return h.Sum(nil)
}

// papWithMA builds the request a RouterOS 7.15+ sends: Message-Authenticator is
// attribute 80, computed over the whole request. good=false breaks it.
func papWithMA(t *testing.T, user, pw string, good bool) (*radius.Packet, []byte) {
	t.Helper()
	p := pap(user, pw)
	p.Add(80, make(radius.Attribute, 16))
	wire, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sum := refMA(t, wire, wire[4:20])
	if !good {
		sum[0] ^= 0xff
	}
	for i := 20; i < len(wire); i += int(wire[i+1]) {
		if wire[i] == 80 {
			copy(wire[i+2:i+18], sum)
		}
	}
	q, err := radius.Parse(wire, []byte("s3cret"))
	if err != nil {
		t.Fatal(err)
	}
	return q, wire
}

// rawExchange sends wire and returns the reply bytes (nil when none came).
func rawExchange(t *testing.T, addr string, wire []byte, wait time.Duration) []byte {
	t.Helper()
	c, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write(wire)
	c.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func TestRepliesCarryMessageAuthenticatorFirst(t *testing.T) {
	cases := map[string]struct {
		v    auth.Verdict
		code radius.Code
	}{
		"reject": {auth.Verdict{Reason: domain.ReasonInvalidCredentials}, radius.CodeAccessReject},
		"accept": {auth.Verdict{Grant: domain.Grant{InterimSeconds: 60, SessionTimeout: 99, DataLimited: true, DataLow: 7, RateLimit: "1k/1k"}}, radius.CodeAccessAccept},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			addr := start(t, &fakeAuth{verdict: c.v})
			for _, withMA := range []bool{false, true} {
				var req *radius.Packet
				var reqWire []byte
				if withMA {
					req, reqWire = papWithMA(t, "alice", "pw", true)
				} else {
					req = pap("alice", "pw")
					reqWire, _ = req.Encode()
				}
				resp := rawExchange(t, addr, reqWire, 2*time.Second)
				if resp == nil {
					t.Fatalf("withMA=%v: no reply", withMA)
				}
				if radius.Code(resp[0]) != c.code {
					t.Fatalf("code %d", resp[0])
				}
				t.Logf("withMA=%v reply %d bytes: %x", withMA, len(resp), resp)
				if resp[20] != 80 || resp[21] != 18 {
					t.Fatalf("first attribute is %d (len %d), want 80 (len 18)", resp[20], resp[21])
				}
				if got, want := resp[22:38], refMA(t, resp, reqWire[4:20]); !bytes.Equal(got, want) {
					t.Fatalf("Message-Authenticator %x, want %x", got, want)
				}
				if !radius.IsAuthenticResponse(resp, reqWire, []byte("s3cret")) {
					t.Fatal("Response Authenticator is wrong")
				}
				if int(resp[2])<<8|int(resp[3]) != len(resp) {
					t.Fatalf("Length field %d, datagram %d", int(resp[2])<<8|int(resp[3]), len(resp))
				}
				_ = req
			}
		})
	}
}

func TestRequestWithBadMessageAuthenticatorDropped(t *testing.T) {
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonInvalidCredentials}}
	link := &fakeLink{}
	ap, _ := net.ListenPacket("udp", "127.0.0.1:0")
	cp, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s := New(staticSecret("s3cret"), fa, &fakeAcct{}, link, slog.Default())
	s.Serve(ap, cp)
	t.Cleanup(func() { s.Shutdown(t.Context()) })
	addr := ap.LocalAddr().String()

	_, bad := papWithMA(t, "alice", "pw", false)
	if rawExchange(t, addr, bad, 400*time.Millisecond) != nil {
		t.Fatal("a request with a bad Message-Authenticator was answered")
	}
	if _, _, bs := link.counts(); bs != 1 {
		t.Fatalf("bad-secret count %d, want 1", bs)
	}
	fa.mu.Lock()
	asked := fa.gotUser
	fa.mu.Unlock()
	if asked != "" {
		t.Fatal("the login check ran for a bad request")
	}
	_, good := papWithMA(t, "alice", "pw", true)
	if rawExchange(t, addr, good, 2*time.Second) == nil {
		t.Fatal("a request with a good Message-Authenticator got no reply")
	}
}

func TestRequestWithoutMessageAuthenticatorStillAnswered(t *testing.T) {
	reply, err := exchange(t, start(t, &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonInvalidCredentials}}), pap("alice", "pw"), 2*time.Second)
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
}

// A reply to an old router (no MA in the request) also carries one, and the
// library still verifies the Response Authenticator.
func TestSealMAReplacesOldOne(t *testing.T) {
	p := radius.New(radius.CodeAccessReject, []byte("s3cret"))
	rfc2865.ReplyMessage_SetString(p, "X")
	p.Add(80, make(radius.Attribute, 16))
	if err := sealMA(p, p.Authenticator); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range p.Attributes {
		if a.Type == 80 {
			n++
		}
	}
	if n != 1 || p.Attributes[0].Type != 80 {
		t.Fatalf("%d Message-Authenticator attributes, first is %d", n, p.Attributes[0].Type)
	}
	if present, ok := checkMA(p, p.Authenticator); !present || !ok {
		t.Fatal("sealed packet does not check")
	}
}
