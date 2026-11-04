package fakenas

import (
	"net"
	"sync"
	"testing"

	"layeh.com/radius"
	"layeh.com/radius/rfc3576"
)

// Reply is what the fake router does with a Disconnect-Request.
type Reply int

const (
	ACK          Reply = iota // Disconnect-ACK
	NAK                       // Disconnect-NAK
	Silent                    // no answer at all
	NAKNoSession              // Disconnect-NAK with Error-Cause 503 (Session-Context-Not-Found)
	ACKBadMA                  // Disconnect-ACK whose Message-Authenticator is wrong
)

// Disconnect is one Disconnect-Request the fake router received.
type Disconnect struct {
	Packet    *radius.Packet
	Authentic bool // the Request Authenticator verifies with the shared secret
	// MAPresent and MAValid: the request has a Message-Authenticator, and it
	// verifies (HMAC-MD5 with a zero Authenticator field, RFC 5176 / RFC 3579 3.2).
	MAPresent, MAValid bool
	From               string
}

// CoA is the router's incoming-RADIUS listener (UDP, normally port 3799): it
// records every Disconnect-Request and answers as told by Behavior.
type CoA struct {
	conn   net.PacketConn
	secret []byte

	mu       sync.Mutex
	got      []Disconnect
	behavior func(n int, d Disconnect) Reply
}

// ListenCoA starts a fake router Disconnect listener on a free loopback UDP
// port. It answers ACK until SetBehavior says otherwise.
func ListenCoA(t *testing.T, secret string) *CoA {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &CoA{conn: conn, secret: []byte(secret)}
	t.Cleanup(func() { conn.Close() })
	go c.serve()
	return c
}

// Port is the UDP port to put into settings.coa_port.
func (c *CoA) Port() int { return c.conn.LocalAddr().(*net.UDPAddr).Port }

// SetBehavior decides the reply to each request; n counts requests from 1.
func (c *CoA) SetBehavior(fn func(n int, d Disconnect) Reply) {
	c.mu.Lock()
	c.behavior = fn
	c.mu.Unlock()
}

// Always answers every request the same way.
func (c *CoA) Always(r Reply) { c.SetBehavior(func(int, Disconnect) Reply { return r }) }

// Requests returns the Disconnect-Requests received so far.
func (c *CoA) Requests() []Disconnect {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Disconnect(nil), c.got...)
}

func (c *CoA) serve() {
	buf := make([]byte, radius.MaxPacketLength)
	for {
		n, from, err := c.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		wire := append([]byte(nil), buf[:n]...)
		p, err := radius.Parse(wire, c.secret)
		if err != nil || p.Code != radius.CodeDisconnectRequest {
			continue
		}
		d := Disconnect{Packet: p, Authentic: radius.IsAuthenticRequest(wire, c.secret), From: from.String()}
		d.MAPresent, d.MAValid = MAStatus(wire, make([]byte, 16), string(c.secret))
		c.mu.Lock()
		c.got = append(c.got, d)
		n2, fn := len(c.got), c.behavior
		c.mu.Unlock()
		reply := ACK
		if fn != nil {
			reply = fn(n2, d)
		}
		code := radius.CodeDisconnectACK
		switch reply {
		case Silent:
			continue
		case NAK, NAKNoSession:
			code = radius.CodeDisconnectNAK
		}
		resp := p.Response(code)
		if reply == NAKNoSession {
			rfc3576.ErrorCause_Set(resp, rfc3576.ErrorCause_Value_SessionContextNotFound)
		}
		// Like a router that follows BlastRADIUS: the reply carries a
		// Message-Authenticator, computed with the request's authenticator.
		resp.Add(80, make(radius.Attribute, 16))
		raw, _ := resp.MarshalBinary()
		sum, _ := ComputeMA(raw, p.Authenticator[:], string(c.secret))
		if reply == ACKBadMA {
			sum[0] ^= 0xff
		}
		copy(resp.Attributes[len(resp.Attributes)-1].Attribute, sum)
		out, err := resp.Encode()
		if err == nil {
			c.conn.WriteTo(out, from)
		}
	}
}
