package auth

import (
	"context"
	"crypto/sha256"
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

// Cipher encrypts and decrypts the stored member password (implemented by secret.Box).
type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

// Evaluator is the member login check of PLAN 7.2, used by the RADIUS handler.
// The rules themselves live in domain.
type Evaluator struct {
	repo   LoginRepo
	cipher Cipher
	clock  clock.Clock
	log    *slog.Logger
	// dummyEnc is a valid ciphertext that an unknown username decrypts, so it costs as much as a known one.
	dummyEnc []byte
}

// NewEvaluator returns an Evaluator that reads from repo and decrypts with cipher.
// It fails when the cipher cannot encrypt, for example with a bad key.
func NewEvaluator(repo LoginRepo, cipher Cipher, clk clock.Clock, log *slog.Logger) (*Evaluator, error) {
	dummyEnc, err := cipher.Encrypt(dummyPassword)
	if err != nil {
		return nil, fmt.Errorf("auth: prepare the dummy password: %w", err)
	}
	return &Evaluator{repo: repo, cipher: cipher, clock: clk, log: log, dummyEnc: dummyEnc}, nil
}

// Verdict is the outcome of a login check: a rejection Reason, or (Reason "")
// the Grant of the Access-Accept.
type Verdict struct {
	Reason domain.Reason
	Grant  domain.Grant
}

// Accepted reports whether the login is allowed.
func (v Verdict) Accepted() bool { return v.Reason == "" }

func reject(r domain.Reason) Verdict { return Verdict{Reason: r} }

// dummyPassword stands in for the stored password of an unknown username.
var dummyPassword = []byte("unknown-user-dummy-password")

// Login runs checks 1-9 of PLAN 7.2 in order. The error is an infrastructure
// failure (database, key): the caller must not answer the router with it.
// MONITOR: no brute-force protection per username at the RADIUS edge (PLAN 9.5).
// MONITOR: check 9 is not atomic with the Accept, so two simultaneous logins at
// max_sessions=1 can both pass, as in legacy.
func (e *Evaluator) Login(ctx context.Context, username, password string) (Verdict, error) {
	m, ok, err := e.authenticate(ctx, username, password) // 1. credentials
	if err != nil {
		return Verdict{}, err
	}
	if !ok {
		return reject(domain.ReasonInvalidCredentials), nil
	}
	// domain.Evaluate repeats this check on purpose (BR-22 and the admin summary).
	if !m.Active { // 2. before the plan checks, as in PLAN 7.2
		return reject(domain.ReasonInactive), nil
	}
	plan, why, err := e.planOf(ctx, m) // 3-4. plan present
	if err != nil {
		return Verdict{}, err
	}
	if why != "" {
		return reject(why), nil
	}
	st, err := e.repo.GetSettings(ctx)
	if err != nil {
		return Verdict{}, err
	}
	now := e.clock.Now()

	// 5-8: expires_at, window (with auto-resubscribe), data, time.
	d := domain.Evaluate(m.State(), plan, now)
	if d.Renew {
		// BR-6: renew, then reload and evaluate again. The renewal is guarded, so if
		// another login renewed first this does nothing. Like legacy, a member
		// rejected for NO_MORE_SESSION has still been renewed.
		if _, err := e.repo.RenewMemberIfUnchanged(ctx, m.ID, m.SubscriptionDate, "auto", now); err != nil {
			return Verdict{}, err
		}
		if m, _, err = e.repo.GetMemberForLogin(ctx, m.Username); err != nil {
			return Verdict{}, err
		}
		if d = domain.Evaluate(m.State(), plan, now); d.Renew {
			// A concurrent edit moved the window again: a normal reject, not a failure.
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
	return Verdict{Grant: domain.GrantFor(m.State(), plan, st.AccountingIntervalSeconds, now)}, nil
}

// authenticate is check 1 of PLAN 7.2: the member exists (BR-1) and the password
// matches. ok is false for invalid credentials; the error is an infrastructure failure.
func (e *Evaluator) authenticate(ctx context.Context, username, password string) (domain.Member, bool, error) {
	name, ok := domain.NormalizeUsername(username)
	if !ok {
		return domain.Member{}, false, nil // BR-1 at the edge
	}
	m, enc, err := e.repo.GetMemberForLogin(ctx, name)
	if errors.Is(err, domain.ErrNotFound) {
		// Do the same work as for a wrong password, so timing does not reveal which usernames exist.
		stored, _ := e.cipher.Decrypt(e.dummyEnc)
		samePassword(stored, password)
		return domain.Member{}, false, nil
	}
	if err != nil {
		return domain.Member{}, false, err
	}
	stored, err := e.cipher.Decrypt(enc)
	if err != nil {
		return domain.Member{}, false, fmt.Errorf("decrypt password of member %d: %w", m.ID, err)
	}
	return m, samePassword(stored, password), nil
}

// samePassword compares the SHA-256 hashes in constant time. Hashing first gives
// equal lengths, so the compare never returns early and the stored length stays hidden.
func samePassword(stored []byte, given string) bool {
	a, b := sha256.Sum256(stored), sha256.Sum256([]byte(given))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// planOf is checks 3-4 of PLAN 7.2. It returns the member's plan, or the Reason why there is none.
func (e *Evaluator) planOf(ctx context.Context, m domain.Member) (domain.Plan, domain.Reason, error) {
	if m.PlanID <= 0 { // 3. defensive: plans are mandatory
		return domain.Plan{}, domain.ReasonNoPlan, nil
	}
	plan, err := e.repo.GetPlan(ctx, m.PlanID)
	if errors.Is(err, domain.ErrNotFound) { // 4. defensive: deleting a used plan is blocked
		return domain.Plan{}, domain.ReasonPlanRemoved, nil
	}
	return plan, "", err
}
