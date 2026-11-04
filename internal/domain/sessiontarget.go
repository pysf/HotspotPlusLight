package domain

import (
	"errors"
	"fmt"
	"strings"
)

// SessionTarget identifies an open router session for a Disconnect-Request (BR-18).
// ID is the session row (0 when unknown); the rest is what the router needs.
type SessionTarget struct {
	ID                                            int64
	NASIP, Username, AcctSessionID, FramedIP, MAC string
}

// Kick failures (BR-18). The wrapped message tells the admin what went wrong.
var (
	ErrKickIdentifiers = errors.New("cannot disconnect")                                                  // the session lacks what a Disconnect needs
	ErrKickRefused     = errors.New("the router refused to disconnect")                                   // Disconnect-NAK
	ErrKickTimeout     = errors.New("the router did not answer")                                          // no reply after the retry
	ErrKickBadReply    = errors.New("the router's answer could not be trusted (check the shared secret)") // reply with a bad Message-Authenticator
	ErrKickNoSecret    = errors.New("the RADIUS shared secret is not set")                                // nothing can be signed
	ErrSessionEnded    = errors.New("this session has already ended")
)

// Validate checks that the session can be addressed: router address, User-Name and
// Acct-Session-Id are required; Framed-IP-Address and Calling-Station-Id are sent
// only when known (BR-18, E5.10).
func (t SessionTarget) Validate() error {
	var missing []string
	if t.NASIP == "" {
		missing = append(missing, "router address")
	}
	if t.Username == "" {
		missing = append(missing, "User-Name")
	}
	if t.AcctSessionID == "" {
		missing = append(missing, "Acct-Session-Id")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: the session has no %s", ErrKickIdentifiers, strings.Join(missing, ", "))
}
