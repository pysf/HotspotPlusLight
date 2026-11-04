// Package fakenas is a fake MikroTik router: a RADIUS client for black-box
// tests. Phase 0 only needs Authenticate; later phases add accounting and a
// Disconnect listener here.
package fakenas

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"errors"
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
	// MessageAuth makes the NAS act like RouterOS 7.15+ (BlastRADIUS): it sends
	// Message-Authenticator in Access-Requests and drops a reply that lacks a
	// correct one. New turns it on, so every e2e run exercises it.
	MessageAuth bool
}

func New(authAddr, secret string) *NAS {
	return &NAS{AuthAddr: authAddr, Secret: secret, Timeout: 3 * time.Second, MessageAuth: true}
}

// ComputeMA is the Message-Authenticator of wire (RFC 3579 3.2), written out
// independently of the server: HMAC-MD5 over the packet with the Authenticator
// field replaced by auth and the first Message-Authenticator value set to zeros.
// It returns the offset of the 16 value bytes, or -1 when there is no such attribute.
func ComputeMA(wire, auth []byte, secret string) (sum []byte, off int) {
	b := append([]byte(nil), wire...)
	copy(b[4:20], auth)
	off = -1
	for i := 20; i+2 <= len(b) && b[i+1] >= 2; i += int(b[i+1]) {
		if b[i] == 80 && b[i+1] == 18 && i+18 <= len(b) {
			off = i + 2
			clear(b[off : off+16])
			break
		}
	}
	h := hmac.New(md5.New, []byte(secret))
	h.Write(b)
	return h.Sum(nil), off
}

// MAStatus reports whether wire has a Message-Authenticator and whether it is
// correct for the given Authenticator (see ComputeMA).
func MAStatus(wire, auth []byte, secret string) (present, ok bool) {
	sum, off := ComputeMA(wire, auth, secret)
	if off < 0 {
		return false, false
	}
	return true, hmac.Equal(sum, wire[off:off+16])
}

// withMA appends a Message-Authenticator to an Access-Request. Call it after
// every other attribute is set.
func (n *NAS) withMA(p *radius.Packet) *radius.Packet {
	if !n.MessageAuth {
		return p
	}
	p.Add(80, make(radius.Attribute, 16))
	wire, _ := p.MarshalBinary()
	sum, _ := ComputeMA(wire, p.Authenticator[:], n.Secret)
	copy(p.Attributes[len(p.Attributes)-1].Attribute, sum)
	return p
}

// PAPPacket builds a PAP Access-Request the way the router sends it.
func (n *NAS) PAPPacket(username, password string) *radius.Packet {
	p := radius.New(radius.CodeAccessRequest, []byte(n.Secret))
	rfc2865.UserName_SetString(p, username)
	rfc2865.UserPassword_SetString(p, password)
	rfc2865.NASIdentifier_SetString(p, "fakenas")
	return n.withMA(p)
}

// Exchange sends p and returns the reply (an error when none arrives, its
// authenticator does not verify with the NAS secret, or, with MessageAuth, it has
// no correct Message-Authenticator as its first attribute).
func (n *NAS) Exchange(p *radius.Packet) (*radius.Packet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), n.Timeout)
	defer cancel()
	reply, err := radius.Exchange(ctx, p, n.AuthAddr)
	if err != nil || !n.MessageAuth {
		return reply, err
	}
	// The library gives the parsed reply; encode it again to get the bytes. The
	// Authenticator field of reply holds the Response Authenticator, and the
	// Message-Authenticator is computed with the request's one in its place.
	wire, err := reply.MarshalBinary()
	if err != nil {
		return nil, err
	}
	if present, ok := MAStatus(wire, p.Authenticator[:], n.Secret); !present || !ok || wire[20] != 80 {
		return nil, errors.New("fakenas: reply has no correct Message-Authenticator as its first attribute (RouterOS would drop it)")
	}
	return reply, nil
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
