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

// SettingsReader gives the shared secret and the router's incoming-RADIUS port
// (read at every Disconnect, so a changed setting applies at once).
type SettingsReader interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
}

// attemptTimeout and attempts give BR-18's "wait at most 3 s, 1 retry".
const (
	attemptTimeout = 1500 * time.Millisecond
	attempts       = 2
)

// Disconnector is the RADIUS Disconnect-Request client (BR-18, PLAN 7.4).
type Disconnector struct {
	settings SettingsReader
	log      *slog.Logger
}

func NewDisconnector(s SettingsReader, log *slog.Logger) *Disconnector {
	return &Disconnector{settings: s, log: log}
}

// Disconnect sends a Disconnect-Request for t to t.NASIP:settings.coa_port,
// signed with the shared secret. It returns nil only for a verified
// Disconnect-ACK; otherwise an error wrapping domain.ErrKickIdentifiers,
// ErrKickRefused (NAK, final; except a NAK with Error-Cause 503
// Session-Context-Not-Found, RFC 5176, which means the router no longer has the
// session: that counts as success so the ghost row can be closed) or ErrKickTimeout (the same packet is sent twice).
//
// VERIFY-ON-HARDWARE: that RouterOS accepts this attribute set (User-Name,
// Acct-Session-Id, Framed-IP-Address, Calling-Station-Id in the router's own
// spelling), that MikroTik sends Error-Cause in its NAK (503 is only honoured when
// present), and needs "incoming" RADIUS enabled on port 3799 (PLAN 13).
func (d *Disconnector) Disconnect(ctx context.Context, t domain.SessionTarget) error {
	if err := t.Validate(); err != nil {
		return err
	}
	st, err := d.settings.GetSettings(ctx)
	if err != nil {
		return err
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
	addr := net.JoinHostPort(t.NASIP, strconv.Itoa(st.CoaPort))
	client := radius.Client{} // no library retry: the loop below resends the same packet
	var last error
	for range attempts {
		actx, cancel := context.WithTimeout(ctx, attemptTimeout)
		reply, err := client.Exchange(actx, p, addr)
		cancel()
		switch {
		case err == nil && reply.Code == radius.CodeDisconnectACK:
			return nil
		case err == nil && reply.Code == radius.CodeDisconnectNAK && rfc3576.ErrorCause_Get(reply) == rfc3576.ErrorCause_Value_SessionContextNotFound:
			d.log.Info("router has no such session (Disconnect-NAK, Error-Cause 503); treating it as disconnected", "user", t.Username, "session", t.AcctSessionID)
			return nil
		case err == nil:
			return fmt.Errorf("%w (%s)", domain.ErrKickRefused, reply.Code)
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			last = domain.ErrKickTimeout
		default:
			return fmt.Errorf("%w: %v", domain.ErrKickTimeout, err) // e.g. ICMP port unreachable: retrying will not help
		}
	}
	return last
}
