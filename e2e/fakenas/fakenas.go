// Package fakenas is a fake MikroTik router: a RADIUS client for black-box
// tests. Phase 0 only needs Authenticate; later phases add accounting and a
// Disconnect listener here.
package fakenas

import (
	"context"
	"net"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

type NAS struct {
	AuthAddr string
	Secret   string
	Timeout  time.Duration
}

func New(authAddr, secret string) *NAS {
	return &NAS{AuthAddr: authAddr, Secret: secret, Timeout: 3 * time.Second}
}

// PAPPacket builds a PAP Access-Request the way the router sends it.
func (n *NAS) PAPPacket(username, password string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, []byte(n.Secret))
	rfc2865.UserName_SetString(p, username)
	rfc2865.UserPassword_SetString(p, password)
	rfc2865.NASIdentifier_SetString(p, "fakenas")
	return p
}

// Exchange sends p and returns the reply (an error when none arrives or its
// authenticator does not verify with the NAS secret).
func (n *NAS) Exchange(p *radius.Packet) (*radius.Packet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), n.Timeout)
	defer cancel()
	return radius.Exchange(ctx, p, n.AuthAddr)
}

// Authenticate sends a PAP Access-Request and returns the reply packet.
func (n *NAS) Authenticate(username, password string) (*radius.Packet, error) {
	return n.Exchange(n.PAPPacket(username, password))
}

// Silent sends p and reports whether the server stayed completely silent for
// the NAS timeout (not even an unverifiable datagram came back).
func (n *NAS) Silent(p *radius.Packet) (bool, error) {
	wire, err := p.Encode()
	if err != nil {
		return false, err
	}
	conn, err := net.Dial("udp", n.AuthAddr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err := conn.Write(wire); err != nil {
		return false, err
	}
	conn.SetReadDeadline(time.Now().Add(n.Timeout))
	buf := make([]byte, radius.MaxPacketLength)
	if _, err := conn.Read(buf); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return true, nil
		}
		return false, err
	}
	return false, nil
}
