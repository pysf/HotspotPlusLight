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
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
)

type NAS struct {
	AuthAddr string
	AcctAddr string // set it to use the accounting methods
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
	return silentUDP(n.AuthAddr, wire, n.Timeout)
}

func silentUDP(addr string, wire []byte, timeout time.Duration) (bool, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err := conn.Write(wire); err != nil {
		return false, err
	}
	conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, radius.MaxPacketLength)
	if _, err := conn.Read(buf); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// Acct describes one accounting packet the way the router sends it. Download
// and Upload are cumulative byte totals for the session from the router's point
// of view (Output = to the client = download, Input = upload); totals above
// 4 GiB are split into octets + Gigawords.
type Acct struct {
	Type             rfc2866.AcctStatusType
	User, SessionID  string
	Download, Upload uint64
	Seconds          uint32
	FramedIP, MAC    string // Calling-Station-Id as the router writes it, e.g. AA:BB:CC:DD:EE:FF
	Cause            rfc2866.AcctTerminateCause
}

// AcctPacket builds an Accounting-Request.
func (n *NAS) AcctPacket(a Acct) *radius.Packet {
	p := radius.New(radius.CodeAccountingRequest, []byte(n.Secret))
	rfc2866.AcctStatusType_Set(p, a.Type)
	if a.User != "" {
		rfc2865.UserName_SetString(p, a.User)
	}
	rfc2866.AcctSessionID_SetString(p, a.SessionID)
	rfc2866.AcctInputOctets_Set(p, rfc2866.AcctInputOctets(uint32(a.Upload)))
	rfc2866.AcctOutputOctets_Set(p, rfc2866.AcctOutputOctets(uint32(a.Download)))
	if a.Upload>>32 > 0 {
		rfc2869.AcctInputGigawords_Set(p, rfc2869.AcctInputGigawords(a.Upload>>32))
	}
	if a.Download>>32 > 0 {
		rfc2869.AcctOutputGigawords_Set(p, rfc2869.AcctOutputGigawords(a.Download>>32))
	}
	rfc2866.AcctSessionTime_Set(p, rfc2866.AcctSessionTime(a.Seconds))
	if a.FramedIP != "" {
		rfc2865.FramedIPAddress_Set(p, net.ParseIP(a.FramedIP).To4())
	}
	if a.MAC != "" {
		rfc2865.CallingStationID_SetString(p, a.MAC)
	}
	if a.Cause != 0 {
		rfc2866.AcctTerminateCause_Set(p, a.Cause)
	}
	rfc2865.NASIdentifier_SetString(p, "fakenas")
	return p
}

// AcctExchange sends p to the accounting port and returns the reply (an error
// when none arrives or it does not verify with the NAS secret).
func (n *NAS) AcctExchange(p *radius.Packet) (*radius.Packet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), n.Timeout)
	defer cancel()
	return radius.Exchange(ctx, p, n.AcctAddr)
}

// Account builds and sends one accounting packet.
func (n *NAS) Account(a Acct) (*radius.Packet, error) { return n.AcctExchange(n.AcctPacket(a)) }

// AcctSilent encodes p, lets tamper change the wire bytes, sends it to the
// accounting port and reports whether the server stayed completely silent for
// the NAS timeout.
func (n *NAS) AcctSilent(p *radius.Packet, tamper func(wire []byte)) (bool, error) {
	wire, err := p.Encode()
	if err != nil {
		return false, err
	}
	if tamper != nil {
		tamper(wire)
	}
	return silentUDP(n.AcctAddr, wire, n.Timeout)
}
