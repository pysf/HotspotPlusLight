package radius

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc3576"

	"hotspotpluslight/internal/domain"
)

// SettingsReader gives the shared secret and the router's incoming-RADIUS port.
// It is read at every Disconnect, so a change applies at once.
type SettingsReader interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
}

// attemptTimeout and attempts give BR-18: wait at most 3 s, with 1 retry.
const (
	attemptTimeout = 1500 * time.Millisecond
	attempts       = 2
)

// DisconnectRecorder receives the outcome of each Disconnect (PLAN 8.2, linkstatus.Recorder).
type DisconnectRecorder interface {
	Disconnect(addr, outcome string)
}

// Disconnector is the RADIUS Disconnect-Request client (BR-18, PLAN 7.4).
type Disconnector struct {
	settings SettingsReader
	link     DisconnectRecorder
	log      *slog.Logger
}

// NewDisconnector returns a Disconnector that reads its settings from s.
func NewDisconnector(s SettingsReader, link DisconnectRecorder, log *slog.Logger) *Disconnector {
	return &Disconnector{settings: s, link: link, log: log}
}

// Disconnect sends a Disconnect-Request for t to t.NASIP on settings.coa_port.
// It returns nil for a verified Disconnect-ACK, and also for a NAK with
// Error-Cause 503 (RFC 5176): the router no longer has the session, so the ghost
// row can be closed. Other errors wrap domain.ErrKickIdentifiers,
// domain.ErrKickRefused (NAK), domain.ErrKickTimeout (no answer after 2 sends, or a network error),
// domain.ErrKickBadReply (replies that fail the Message-Authenticator check) or
// domain.ErrKickNoSecret (no shared secret is set; nothing is sent).
//
// VERIFY-ON-HARDWARE: RouterOS accepts User-Name, Acct-Session-Id, Framed-IP-Address and
// Calling-Station-Id (in its own MAC spelling), sends Error-Cause in its NAK (503 needs it), and has
// "incoming" RADIUS enabled on port 3799 (PLAN 13).
func (d *Disconnector) Disconnect(ctx context.Context, t domain.SessionTarget) error {
	if err := t.Validate(); err != nil {
		return err
	}
	st, err := d.settings.GetSettings(ctx)
	if err != nil {
		return err
	}
	if st.RadiusSharedSecret == "" { // the router would drop the packet and the admin would wait for a timeout
		d.link.Disconnect(net.JoinHostPort(t.NASIP, strconv.Itoa(st.CoaPort)), "shared secret not set")
		return domain.ErrKickNoSecret
	}
	p := radius.New(radius.CodeDisconnectRequest, []byte(st.RadiusSharedSecret))
	rfc2865.UserName_SetString(p, t.Username)
	rfc2866.AcctSessionID_SetString(p, t.AcctSessionID)
	if ip := net.ParseIP(t.FramedIP).To4(); ip != nil {
		rfc2865.FramedIPAddress_Set(p, ip)
	}
	if t.MAC != "" {
		rfc2865.CallingStationID_SetString(p, t.MAC)
	}
	// RFC 5176 allows Message-Authenticator and RouterOS 7.15+ may require it
	// (BlastRADIUS). It is computed with a zero Authenticator field, because the
	// Request Authenticator that Encode computes afterwards covers it (RFC 3579 3.2).
	// VERIFY-ON-HARDWARE: RouterOS accepts it on incoming RADIUS (port 3799).
	p.Authenticator = [16]byte{}
	if err := sealMA(p, p.Authenticator); err != nil {
		return err
	}
	wire, err := p.Encode() // the same bytes Exchange sends; wire[4:20] is the Request Authenticator
	if err != nil {
		return err
	}
	var reqAuth [16]byte
	copy(reqAuth[:], wire[4:20])
	addr := net.JoinHostPort(t.NASIP, strconv.Itoa(st.CoaPort))
	client := radius.Client{} // no library retry: the loop below resends the same packet
	var last error
	// MONITOR: the panel's outcome is best effort. A cancelled context shows as "no answer",
	// and a failure before the packet is built is not recorded.
	outcome := "no answer"
	defer func() {
		d.link.Disconnect(addr, outcome)
		level := slog.LevelWarn // anything but an ACK or "session not found" means the member may still be online
		if outcome == "ACK" || outcome == "NAK (session not found)" {
			level = slog.LevelInfo
		}
		d.log.Log(ctx, level, "disconnect", "addr", addr, "user", t.Username, "session", t.AcctSessionID, "outcome", outcome)
	}()
	for range attempts {
		actx, cancel := context.WithTimeout(ctx, attemptTimeout)
		reply, err := client.Exchange(actx, p, addr)
		cancel()
		if err == nil {
			// A reply with a Message-Authenticator must carry a correct one (RFC 3579 3.2:
			// the Authenticator field holds the request's authenticator).
			if present, ok := checkMA(reply, reqAuth); present && !ok {
				d.log.Warn("Disconnect reply ignored: its Message-Authenticator does not verify", "addr", addr)
				last, outcome = domain.ErrKickBadReply, "bad reply (check the shared secret)" // not believed, so try again
				continue
			}
		}
		switch {
		case err == nil && reply.Code == radius.CodeDisconnectACK:
			outcome = "ACK"
			return nil
		case err == nil && reply.Code == radius.CodeDisconnectNAK && rfc3576.ErrorCause_Get(reply) == rfc3576.ErrorCause_Value_SessionContextNotFound:
			d.log.Info("router has no such session (Disconnect-NAK, Error-Cause 503); treating it as disconnected", "user", t.Username, "session", t.AcctSessionID)
			outcome = "NAK (session not found)"
			return nil
		case err == nil:
			outcome = "NAK"
			if ec, err := rfc3576.ErrorCause_Lookup(reply); err == nil {
				outcome = fmt.Sprintf("NAK (Error-Cause %d)", ec)
			}
			return fmt.Errorf("%w (%s)", domain.ErrKickRefused, reply.Code)
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			if last == nil { // a bad reply is more useful to the admin than "no answer"
				last = domain.ErrKickTimeout
			}
		default:
			return fmt.Errorf("%w: %v", domain.ErrKickTimeout, err) // e.g. ICMP port unreachable: retrying will not help
		}
	}
	return last
}
