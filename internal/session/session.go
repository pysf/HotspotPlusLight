// Package session handles accounting (BR-11, BR-12), the online list, kick
// (BR-18), server-side enforcement and pruning of old sessions (O18).
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
	// LatestSession returns the latest open session of the key, else the latest
	// finished one; nil, nil when there is none.
	LatestSession(ctx context.Context, username, sessionID, nasIP string) (*domain.Session, error)
	InsertSession(ctx context.Context, s domain.Session) error
	UpdateSession(ctx context.Context, s domain.Session) error
	AddUsage(ctx context.Context, memberID int64, d domain.Counters, now time.Time) error
	// AddDailyUsage adds the same delta to the member's row of the given day (D12).
	AddDailyUsage(ctx context.Context, memberID int64, day string, d domain.Counters) error
}

// Repo is what the Service needs from storage (implemented by store).
type Repo interface {
	CountOpenSessions(ctx context.Context) (int, error)
	// InAcctTx runs fn in one transaction: committed when fn returns nil, rolled back otherwise.
	InAcctTx(ctx context.Context, fn func(AcctTx) error) error
	SessionRetentionDays(ctx context.Context) (int, error)
	PruneSessions(ctx context.Context, cutoff time.Time) (int, error)
	GetSettings(ctx context.Context) (domain.Settings, error)
	CountOnlineSessions(ctx context.Context, since time.Time) (int, error)
	ListOnlineSessions(ctx context.Context, since time.Time, limit, offset int) ([]domain.Session, error)
	// SessionTargetByID reports whether the row is still open; domain.ErrNotFound if unknown.
	SessionTargetByID(ctx context.Context, id int64) (t domain.SessionTarget, open bool, err error)
	EndSession(ctx context.Context, id int64, now time.Time, cause string) error
}

// Target identifies a session to disconnect (BR-18). It lives in domain so the
// member service does not import this package.
type Target = domain.SessionTarget

// Disconnector sends a Disconnect-Request and returns nil only on a
// Disconnect-ACK (implemented by internal/radius). Errors wrap domain.ErrKick*.
type Disconnector interface {
	Disconnect(ctx context.Context, t Target) error
}

// KickCause is the terminate cause of a session ended by an ACKed kick (BR-18).
const KickCause = "Admin-Reset"

// Disconnect sends a Disconnect-Request for t and, on ACK, ends the session row.
// On NAK or timeout the row is unchanged and the error is returned (BR-18).
// The member service uses it (deactivate, delete).
func (s *Service) Disconnect(ctx context.Context, t Target) error {
	return s.DisconnectWithCause(ctx, t, KickCause)
}

// DisconnectWithCause is Disconnect with the terminate cause the row gets on
// ACK. The enforcement uses "Server: <CODE>" (O3/O4).
func (s *Service) DisconnectWithCause(ctx context.Context, t Target, cause string) error {
	if err := s.disc.Disconnect(ctx, t); err != nil {
		return err
	}
	if t.ID == 0 {
		return nil
	}
	// The user is gone. If the row cannot be ended, still succeed: a retry would send
	// a second Disconnect, and the router's Stop closes the row later.
	if err := s.repo.EndSession(ctx, t.ID, s.clock.Now(), cause); err != nil {
		s.log.Error("disconnect ACKed but the session row could not be marked ended; the router's Stop will close it", "session_id", t.ID, "err", err)
	}
	return nil
}

// Kick disconnects one session by row id. It returns domain.ErrNotFound if the
// id is unknown and domain.ErrSessionEnded if the session is already finished.
func (s *Service) Kick(ctx context.Context, id int64) error {
	t, open, err := s.repo.SessionTargetByID(ctx, id)
	if err != nil {
		return err
	}
	if !open {
		return domain.ErrSessionEnded
	}
	return s.Disconnect(ctx, t)
}

// OnlineView is one page of the online list (BR-17).
type OnlineView struct {
	Rows                    []domain.Session
	Total, Page, Pages, Per int
	IntervalSeconds         int // settings.accounting_interval_seconds, shown as "updated every N s"
	Now                     time.Time
	Loc                     *time.Location
}

// Online returns one page of the online sessions, newest first. "Online" is
// the same rule as the dot in the member list (domain.ActiveSince).
func (s *Service) Online(ctx context.Context, page, per int) (OnlineView, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return OnlineView{}, err
	}
	loc, err := st.Location()
	if err != nil {
		return OnlineView{}, err
	}
	now := s.clock.Now()
	since := domain.ActiveSince(now, st.AccountingIntervalSeconds)
	total, err := s.repo.CountOnlineSessions(ctx, since)
	if err != nil {
		return OnlineView{}, err
	}
	page, per, pages := domain.NormalizePaging(page, per, total)
	rows, err := s.repo.ListOnlineSessions(ctx, since, per, (page-1)*per)
	if err != nil {
		return OnlineView{}, err
	}
	return OnlineView{Rows: rows, Total: total, Page: page, Pages: pages, Per: per, IntervalSeconds: st.AccountingIntervalSeconds, Now: now, Loc: loc}, nil
}

// Service is the accounting, online list, kick and pruning service.
type Service struct {
	repo         Repo
	disc         Disconnector
	clock        clock.Clock
	log          *slog.Logger
	beforeCommit func() error
	enforcer     Checker
}

// Checker asks for an enforcement check of one open session (implemented by *Enforcer).
type Checker interface {
	Check(memberID int64, username, acctSessionID, nasIP string)
}

// WithEnforcer makes Account ask c to check the open session after a Start or
// Interim is stored (layer 2 of the enforcement, O3/O4).
func WithEnforcer(c Checker) Option { return func(s *Service) { s.enforcer = c } }

// Option customizes a Service.
type Option func(*Service)

// WithBeforeCommit sets a hook that runs inside every accounting transaction
// just before the commit. An error rolls it back (crash test E4.15).
func WithBeforeCommit(fn func() error) Option { return func(s *Service) { s.beforeCommit = fn } }

// WithLogger sets the logger (default: discard).
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// New returns a Service. Without WithLogger, logs are discarded.
func New(repo Repo, disc Disconnector, clk clock.Clock, opts ...Option) *Service {
	s := &Service{repo: repo, disc: disc, clock: clk, log: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(s)
	}
	return s
}

// OpenCount returns how many sessions are open.
func (s *Service) OpenCount(ctx context.Context) (int, error) { return s.repo.CountOpenSessions(ctx) }

// Account applies one Start, Interim or Stop record (PLAN 7.3, BR-11, BR-12) in
// one transaction. On nil the change is committed and the caller may send the
// Accounting-Response; on error nothing was stored and no reply must be sent.
// Other status types and unknown users change nothing.
//
// After the commit, a Start or Interim of an open session asks the Enforcer to
// check the member (O3/O4). A Stop never does.
//
// MONITOR: Accounting-On/Off do nothing (owner, 2026-11-01). After a router
// reboot, ghost sessions can block max_sessions until they go stale (BR-9) or
// the admin kicks them.
func (s *Service) Account(ctx context.Context, rec domain.AcctRecord) error {
	switch rec.Status {
	case domain.AcctStart, domain.AcctInterim, domain.AcctStop:
	default:
		return nil
	}
	now := s.clock.Now()
	mac, ok := domain.NormalizeMAC(rec.MAC)
	if !ok && rec.MAC != "" { // keep going: the packet is still accounted, without a MAC
		raw := rec.MAC
		if len(raw) > 40 {
			raw = raw[:40]
		}
		s.log.Warn("unparsable Calling-Station-Id in accounting; stored without MAC", "session", rec.SessionID, "value", raw)
	}
	rec.MAC = mac
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	loc, err := st.Location()
	if err != nil { // usage history must never block quota accounting
		s.log.Warn("invalid settings timezone; booking usage history in UTC", "err", err)
		loc = time.UTC
	}
	day := domain.DayKey(now, loc)
	var check *domain.Session // checked by the Enforcer after the commit
	var checkMember int64
	err = s.repo.InAcctTx(ctx, func(tx AcctTx) error {
		check = nil
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
				if err := tx.AddDailyUsage(ctx, m.ID, day, res.Delta); err != nil {
					return err
				}
			}
			if m.MAC == "" && rec.MAC != "" { // BR-12: first packet only, never overwritten
				if err := tx.SetMemberMACIfEmpty(ctx, m.ID, rec.MAC, now); err != nil {
					return err
				}
			}
		}
		if !res.Ignore && rec.Status != domain.AcctStop && res.Session.EndedAt == nil {
			c := res.Session
			check, checkMember = &c, m.ID
		}
		if s.beforeCommit != nil {
			return s.beforeCommit()
		}
		return nil
	})
	if err == nil && check != nil && s.enforcer != nil {
		s.enforcer.Check(checkMember, check.Username, check.AcctSessionID, check.NASIP)
	}
	return err
}

// Prune deletes sessions older than settings.session_retention_days (O18) and
// returns how many it deleted. Retention 0 keeps everything. Member usage is on
// the member row and is not affected.
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
