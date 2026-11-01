// Package fakenas is a fake MikroTik router: a RADIUS client for black-box
// tests. Phase 0 only needs Authenticate; later phases add accounting and a
// Disconnect listener here.
package fakenas

import (
	"context"
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

// Authenticate sends a PAP Access-Request and returns the reply packet.
func (n *NAS) Authenticate(username, password string) (*radius.Packet, error) {
	p := radius.New(radius.CodeAccessRequest, []byte(n.Secret))
	rfc2865.UserName_SetString(p, username)
	rfc2865.UserPassword_SetString(p, password)
	rfc2865.NASIdentifier_SetString(p, "fakenas")
	ctx, cancel := context.WithTimeout(context.Background(), n.Timeout)
	defer cancel()
	return radius.Exchange(ctx, p, n.AuthAddr)
}
