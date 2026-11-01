// Package session is the accounting / online list / kick / pruning service.
// Phase 4 adds accounting (BR-11, BR-12) and pruning (O18); Phase 5 the online
// list and kick.
package session

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// AcctTx is what one accounting packet needs from the database. All calls of
// one packet share a single transaction.
type AcctTx interface {
	MemberByUsername(ctx context.Context, username string) (domain.Member, error) // domain.ErrNotFound if unknown
	SetMemberMACIfEmpty(ctx context.Context, id int64, mac string, now time.Time) error
	// LatestSession returns the latest open session of the identity key, else the
	// latest finished one; nil, nil when there is none.
	LatestSession(ctx context.Context, username, sessionID, nasIP string) (*domain.Session, error)
	InsertSession(ctx context.Context, s domain.Session) error
	UpdateSession(ctx context.Context, s domain.Session) error
	AddUsage(ctx context.Context, memberID int64, d domain.Counters, now time.Time) error
}

type Repo interface {
	CountOpenSessions(ctx context.Context) (int, error)
	// InAcctTx runs fn in one transaction: committed when fn returns nil, rolled back otherwise.
	InAcctTx(ctx context.Context, fn func(AcctTx) error) error
	SessionRetentionDays(ctx context.Context) (int, error)
	PruneSessions(ctx context.Context, cutoff time.Time) (int, error)
}

// Target identifies a session to disconnect (BR-18). The type lives in domain
// so that the member service can use it without importing this package.
type Target = domain.SessionTarget

// Disconnector sends a RADIUS Disconnect-Request (implemented in Phase 5 by internal/radius).
type Disconnector interface {
	Disconnect(ctx context.Context, t Target) error
}

type Service struct {
	repo         Repo
	disc         Disconnector
	clock        clock.Clock
	log          *slog.Logger
	beforeCommit func() error
}

// Option customizes a Service.
type Option func(*Service)

// WithBeforeCommit installs a hook that runs inside every accounting
// transaction right before it commits; an error rolls the transaction back
// (crash injection for E4.15).
func WithBeforeCommit(fn func() error) Option { return func(s *Service) { s.beforeCommit = fn } }

// WithLogger sets the logger (default: discard).
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

func New(repo Repo, disc Disconnector, clk clock.Clock, opts ...Option) *Service {
	s := &Service{repo: repo, disc: disc, clock: clk, log: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Service) OpenCount(ctx context.Context) (int, error) { return s.repo.CountOpenSessions(ctx) }

// Account applies one Start/Interim/Stop record (PLAN 7.3, BR-11, BR-12) in a
// single transaction. When it returns nil the change is committed and the
// caller may send the Accounting-Response; on error nothing was stored and no
// reply must be sent. Other status types and unknown users change nothing.
//
// MONITOR: legacy-faithful gap (O3/O4): nothing here ends a session whose quota
// is used up. The router's Mikrotik-Xmit-Limit and Session-Timeout (set at
// login) are the only enforcement; the next Access-Request is rejected by the
// login check. Revisit if routers do not enforce them (PLAN 13.2, 13.4).
//
// MONITOR: Accounting-On/Off are acknowledged without effect (owner,
// 2026-11-01): after a router reboot, ghost sessions can block max_sessions
// logins until they go stale (BR-9) or the admin kicks them.
func (s *Service) Account(ctx context.Context, rec domain.AcctRecord) error {
	switch rec.Status {
	case domain.AcctStart, domain.AcctInterim, domain.AcctStop:
	default:
		return nil
	}
	now := s.clock.Now()
	mac, ok := domain.NormalizeMAC(rec.MAC)
	if !ok && rec.MAC != "" { // stored as empty; the packet is still accounted
		raw := rec.MAC
		if len(raw) > 40 {
			raw = raw[:40]
		}
		s.log.Warn("unparsable Calling-Station-Id in accounting; stored without MAC", "session", rec.SessionID, "value", raw)
	}
	rec.MAC = mac
	return s.repo.InAcctTx(ctx, func(tx AcctTx) error {
		m, err := tx.MemberByUsername(ctx, rec.Username)
		if errors.Is(err, domain.ErrNotFound) {
			return nil // unknown user: acknowledged, nothing stored (legacy)
		}
		if err != nil {
			return err
		}
		rec.Username = m.Username // canonical spelling, so the key matches across packets
		latest, err := tx.LatestSession(ctx, rec.Username, rec.SessionID, rec.NASIP)
		if err != nil {
			return err
		}
		res := domain.ApplyAccounting(latest, rec, now)
		if !res.Ignore {
			if res.Closed != nil {
				if err := tx.UpdateSession(ctx, *res.Closed); err != nil {
					return err
				}
			}
			res.Session.MemberID = m.ID
			if res.Insert {
				err = tx.InsertSession(ctx, res.Session)
			} else {
				err = tx.UpdateSession(ctx, res.Session)
			}
			if err != nil {
				return err
			}
			if !res.Delta.IsZero() {
				if err := tx.AddUsage(ctx, m.ID, res.Delta, now); err != nil {
					return err
				}
			}
			if m.MAC == "" && rec.MAC != "" { // BR-12: first packet only, never overwritten
				if err := tx.SetMemberMACIfEmpty(ctx, m.ID, rec.MAC, now); err != nil {
					return err
				}
			}
		}
		if s.beforeCommit != nil {
			return s.beforeCommit()
		}
		return nil
	})
}

// Prune deletes sessions older than settings.session_retention_days (O18;
// 0 = keep forever) and returns how many went. Member usage counters are not
// affected: they live on the member row.
func (s *Service) Prune(ctx context.Context) (int, error) {
	days, err := s.repo.SessionRetentionDays(ctx)
	if err != nil || days <= 0 {
		return 0, err
	}
	n, err := s.repo.PruneSessions(ctx, s.clock.Now().Add(-time.Duration(days)*24*time.Hour))
	if err == nil && n > 0 {
		s.log.Info("old sessions pruned", "count", n, "retention_days", days)
	}
	return n, err
}
