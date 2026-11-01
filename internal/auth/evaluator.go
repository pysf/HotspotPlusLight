package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// LoginRepo is what the Evaluator needs from storage (implemented by store).
type LoginRepo interface {
	// GetMemberForLogin is case-insensitive on the username; domain.ErrNotFound if unknown.
	GetMemberForLogin(ctx context.Context, username string) (domain.Member, []byte, error)
	// GetPlan returns domain.ErrNotFound if the plan does not exist.
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	GetSettings(ctx context.Context) (domain.Settings, error)
	CountActiveSessions(ctx context.Context, memberID int64, since time.Time) (int, error)
	// RenewMemberIfUnchanged renews (BR-13b) in one statement only if the member's
	// window start is still subscription; it reports whether it did.
	RenewMemberIfUnchanged(ctx context.Context, id int64, subscription time.Time, kind string, now time.Time) (bool, error)
}

// Cipher decrypts the stored member password (implemented by secret.Box).
type Cipher interface {
	Decrypt(data []byte) ([]byte, error)
}

// Evaluator is THE member login check (PLAN 7.2), used by the RADIUS handler
// and (Phase 6) the page API. The rules themselves live in domain.
type Evaluator struct {
	repo   LoginRepo
	cipher Cipher
	clock  clock.Clock
	log    *slog.Logger
}

func NewEvaluator(repo LoginRepo, cipher Cipher, clk clock.Clock, log *slog.Logger) *Evaluator {
	return &Evaluator{repo: repo, cipher: cipher, clock: clk, log: log}
}

// Verdict is the outcome of a login check: a rejection Reason, or (Reason "")
// the Grant of the Access-Accept.
type Verdict struct {
	Reason domain.Reason
	Grant  domain.Grant
}

func (v Verdict) Accepted() bool { return v.Reason == "" }

func reject(r domain.Reason) Verdict { return Verdict{Reason: r} }

// dummyPassword stands in for the stored password of an unknown username.
var dummyPassword = []byte("unknown-user-dummy-password")

// Login runs checks 1-9 of PLAN 7.2 in order. The error is an infrastructure
// failure (database, key): the caller must not answer the router with it
// (no false Accept, no false Reject).
// MONITOR: there is no brute-force protection per username at the RADIUS edge
// (PLAN 9.5, accepted by the owner).
// MONITOR: check 9 (concurrent sessions) is not atomic with the Accept: two
// simultaneous logins at max_sessions=1 can both pass, as in legacy (no locking).
func (e *Evaluator) Login(ctx context.Context, username, password string) (Verdict, error) {
	name, ok := domain.NormalizeUsername(username)
	if !ok {
		return reject(domain.ReasonInvalidCredentials), nil // BR-1 at the edge
	}
	m, enc, err := e.repo.GetMemberForLogin(ctx, name)
	if errors.Is(err, domain.ErrNotFound) {
		// Same work as for a known user with a wrong password, so timing does not
		// reveal which usernames exist.
		subtle.ConstantTimeCompare(dummyPassword, []byte(password))
		return reject(domain.ReasonInvalidCredentials), nil
	}
	if err != nil {
		return Verdict{}, err
	}
	stored, err := e.cipher.Decrypt(enc)
	if err != nil {
		return Verdict{}, fmt.Errorf("decrypt password of member %d: %w", m.ID, err)
	}
	if subtle.ConstantTimeCompare(stored, []byte(password)) != 1 { // 1. credentials
		return reject(domain.ReasonInvalidCredentials), nil
	}
	if m.PlanID <= 0 { // 3. defensive: plans are mandatory
		return reject(domain.ReasonNoPlan), nil
	}
	plan, err := e.repo.GetPlan(ctx, m.PlanID)
	if errors.Is(err, domain.ErrNotFound) { // 4. defensive: deleting a used plan is blocked
		return reject(domain.ReasonPlanRemoved), nil
	}
	if err != nil {
		return Verdict{}, err
	}
	st, err := e.repo.GetSettings(ctx)
	if err != nil {
		return Verdict{}, err
	}
	now := e.clock.Now()

	// 2, 5-8: active, expires_at, window (with auto-resubscribe), data, time.
	d := domain.Evaluate(m.State(), plan, now)
	if d.Renew {
		// BR-6: renew, then re-evaluate against the stored result. If another
		// login renewed first, our guarded renewal does nothing and we simply
		// reload the member it renewed. Legacy order (checkAuthorization renews
		// before the session check): a member rejected for NO_MORE_SESSION has
		// still been renewed.
		if _, err := e.repo.RenewMemberIfUnchanged(ctx, m.ID, m.SubscriptionDate, "auto", now); err != nil {
			return Verdict{}, err
		}
		if m, _, err = e.repo.GetMemberForLogin(ctx, name); err != nil {
			return Verdict{}, err
		}
		if d = domain.Evaluate(m.State(), plan, now); d.Renew {
			// Concurrent edit moved the window again: a normal reject, not a failure.
			e.log.Warn("member still outside its window after renewal; rejecting", "member", m.ID)
			return reject(domain.ReasonNoSubscription), nil
		}
	}
	if !d.Allowed() {
		return reject(d.Reason), nil
	}

	// 9. concurrent sessions of the member's plan.
	active, err := e.repo.CountActiveSessions(ctx, m.ID, domain.ActiveSince(now, st.AccountingIntervalSeconds))
	if err != nil {
		return Verdict{}, err
	}
	if active >= plan.MaxSessions {
		return reject(domain.ReasonNoMoreSession), nil
	}
	return Verdict{Grant: domain.GrantFor(domain.QuotaOf(m.State(), plan), plan, st.AccountingIntervalSeconds)}, nil
}
