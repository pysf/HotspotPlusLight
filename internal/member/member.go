// Package member is the member service: create, edit, renew, extra bulk,
// passwords, list and delete (kicking open sessions first).
package member

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/secret"
)

// Repo is what the service needs from storage (implemented by store).
// Create/Update return domain.ErrUsernameTaken on a case-insensitive duplicate;
// Get*/Update/Set*/Renew return domain.ErrNotFound for an unknown id.
type Repo interface {
	CreateMember(ctx context.Context, in domain.MemberInput, passwordEnc []byte, now time.Time) (int64, error)
	GetMember(ctx context.Context, id int64) (domain.Member, error)
	GetMemberRow(ctx context.Context, id int64, onlineSince time.Time) (domain.MemberRow, error)
	UpdateMember(ctx context.Context, id int64, in domain.MemberInput, now time.Time) error
	GetMemberPassword(ctx context.Context, id int64) ([]byte, error)
	SetMemberPassword(ctx context.Context, id int64, passwordEnc []byte, now time.Time) error
	RenewMember(ctx context.Context, id int64, kind string, now time.Time) error
	SetExtraBulk(ctx context.Context, id int64, bytes int64, now time.Time) error
	CountFiltered(ctx context.Context, f domain.MemberFilter, now time.Time) (int, error)
	ListMembers(ctx context.Context, f domain.MemberFilter, limit, offset int, now, onlineSince time.Time) ([]domain.MemberRow, error)
	SelectionStats(ctx context.Context, sel domain.MemberSelection, now, onlineSince time.Time) (members, online int, err error)
	OpenSessions(ctx context.Context, sel domain.MemberSelection, now time.Time) ([]domain.SessionTarget, error)
	// DeleteMembers returns how many members it deleted and the open sessions
	// they had inside the same transaction.
	DeleteMembers(ctx context.Context, sel domain.MemberSelection, now time.Time) (int, []domain.SessionTarget, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	GetSettings(ctx context.Context) (domain.Settings, error)
}

// Cipher encrypts member passwords at rest (implemented by secret.Box).
type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

// Disconnector ends a router session (BR-18); implemented in Phase 5 by the RADIUS CoA client.
type Disconnector interface {
	Disconnect(ctx context.Context, t domain.SessionTarget) error
}

type Service struct {
	repo   Repo
	cipher Cipher
	disc   Disconnector
	clock  clock.Clock
}

func New(repo Repo, cipher Cipher, disc Disconnector, clk clock.Clock) *Service {
	return &Service{repo: repo, cipher: cipher, disc: disc, clock: clk}
}

// env is the per-request context every operation needs: settings, display zone, now.
type env struct {
	settings    domain.Settings
	loc         *time.Location
	now         time.Time
	onlineSince time.Time // sessions updated since then count as online (BR-17)
}

func (s *Service) env(ctx context.Context) (env, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return env{}, err
	}
	loc, lerr := time.LoadLocation(st.Timezone)
	if lerr != nil { // settings validation rejects bad zones, so this is a corrupt-settings error
		return env{}, fmt.Errorf("settings.timezone %q: %w", st.Timezone, lerr)
	}
	now := s.clock.Now()
	return env{st, loc, now, now.Add(-time.Duration(st.AccountingIntervalSeconds+60) * time.Second)}, nil
}

// View is a member with everything the admin pages show about it.
type View struct {
	Row     domain.MemberRow
	Summary domain.Summary
	Loc     *time.Location
}

func newView(r domain.MemberRow, e env) View {
	return View{Row: r, Summary: domain.Summarize(r.Member, r.Plan, e.now), Loc: e.loc}
}

// NewForm is the form for a new member: the default plan is preselected, never assigned (O10, Q19).
func (s *Service) NewForm(ctx context.Context) (domain.MemberForm, error) {
	st, err := s.repo.GetSettings(ctx)
	return domain.NewMemberForm(st.DefaultPlanID), err
}

// Location is the display zone (settings.timezone).
func (s *Service) Location(ctx context.Context) (*time.Location, error) {
	e, err := s.env(ctx)
	return e.loc, err
}

// planError returns a field message when the chosen plan does not exist.
func (s *Service) planError(ctx context.Context, planID int64) (domain.FieldErrors, error) {
	if _, err := s.repo.GetPlan(ctx, planID); errors.Is(err, domain.ErrNotFound) {
		return domain.FieldErrors{"plan_id": "Choose a plan from the list."}, nil
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

// Create validates and stores a member (BR-14). A non-empty FieldErrors means nothing was stored.
func (s *Service) Create(ctx context.Context, f domain.MemberForm) (int64, domain.FieldErrors, error) {
	e, err := s.env(ctx)
	if err != nil {
		return 0, nil, err
	}
	in, errs := domain.ParseMember(f, e.loc, true)
	password := f.Password
	if strings.TrimSpace(password) == "" {
		if password, err = secret.RandomDigits(6); err != nil { // BR-14: random 6-digit number
			return 0, nil, err
		}
	} else if msg := domain.ValidateMemberPassword(password); msg != "" {
		errs["password"] = msg
	}
	if len(errs) == 0 {
		errs, err = s.planError(ctx, in.PlanID)
		if err != nil {
			return 0, nil, err
		}
	}
	if len(errs) > 0 {
		return 0, errs, nil
	}
	enc, err := s.cipher.Encrypt([]byte(password))
	if err != nil {
		return 0, nil, err
	}
	id, err := s.repo.CreateMember(ctx, in, enc, e.now)
	if errors.Is(err, domain.ErrUsernameTaken) {
		return 0, domain.FieldErrors{"username": "This username is already taken."}, nil
	}
	return id, nil, err
}

// Get returns one member with its summary (domain.ErrNotFound if unknown).
func (s *Service) Get(ctx context.Context, id int64) (View, error) {
	e, err := s.env(ctx)
	if err != nil {
		return View{}, err
	}
	r, err := s.repo.GetMemberRow(ctx, id, e.onlineSince)
	if err != nil {
		return View{}, err
	}
	return newView(r, e), nil
}

// KickResult counts the Disconnect-Requests sent and how many failed (BR-18).
type KickResult struct{ Attempted, Failed int }

// kick sends a Disconnect for every open session of the selection. Failures are
// counted, never fatal: the caller carries on (BR-18, BR-23). It returns the
// targets it tried.
// VERIFY-ON-HARDWARE: the real Disconnect client arrives in Phase 5.
func (s *Service) kick(ctx context.Context, sel domain.MemberSelection, now time.Time) (KickResult, []domain.SessionTarget, error) {
	targets, err := s.repo.OpenSessions(ctx, sel, now)
	if err != nil {
		return KickResult{}, nil, err
	}
	var res KickResult
	s.disconnect(ctx, targets, &res)
	return res, targets, nil
}

// disconnect sends a Disconnect for each target and adds the outcome to res.
func (s *Service) disconnect(ctx context.Context, targets []domain.SessionTarget, res *KickResult) {
	for _, t := range targets {
		res.Attempted++
		if s.disc.Disconnect(ctx, t) != nil {
			res.Failed++
		}
	}
}

// Update validates and stores the member's editable fields. Changing the plan
// only changes plan_id (BR-13a). Deactivating kicks the open sessions, and the
// member is inactive whatever the kick result (BR-18, Q14).
func (s *Service) Update(ctx context.Context, id int64, f domain.MemberForm) (domain.FieldErrors, KickResult, error) {
	e, err := s.env(ctx)
	if err != nil {
		return nil, KickResult{}, err
	}
	before, err := s.repo.GetMember(ctx, id)
	if err != nil {
		return nil, KickResult{}, err
	}
	in, errs := domain.ParseMember(f, e.loc, false)
	if len(errs) == 0 {
		if errs, err = s.planError(ctx, in.PlanID); err != nil {
			return nil, KickResult{}, err
		}
	}
	if len(errs) > 0 {
		return errs, KickResult{}, nil
	}
	if in.Username != before.Username { // the accounting key of a session is its username
		open, err := s.repo.OpenSessions(ctx, domain.MemberSelection{IDs: []int64{id}}, e.now)
		if err != nil {
			return nil, KickResult{}, err
		}
		if len(open) > 0 {
			return domain.FieldErrors{"username": "This member is online: disconnect the member before renaming."}, KickResult{}, nil
		}
	}
	// MONITOR: an expires_at in the past does not disconnect a member who is online (O3/O7: enforced at login only).
	err = s.repo.UpdateMember(ctx, id, in, e.now)
	if errors.Is(err, domain.ErrUsernameTaken) {
		return domain.FieldErrors{"username": "This username is already taken."}, KickResult{}, nil
	}
	if err != nil {
		return nil, KickResult{}, err
	}
	var kick KickResult
	if before.Active && !in.Active {
		kick, _, err = s.kick(ctx, domain.MemberSelection{IDs: []int64{id}}, e.now)
	}
	return nil, kick, err
}

// Renew restarts the member's validity window by hand (BR-13b, kind "manual").
func (s *Service) Renew(ctx context.Context, id int64) error {
	return s.repo.RenewMember(ctx, id, "manual", s.clock.Now())
}

// SetExtraBulk sets (not adds) the member's extra bulk (Q17). Errors: domain.ErrNotFound,
// domain.ErrUnlimitedData (plan has unlimited data); validation problems come back as FieldErrors.
func (s *Service) SetExtraBulk(ctx context.Context, id int64, value, unit string) (domain.FieldErrors, error) {
	e, err := s.env(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.repo.GetMemberRow(ctx, id, e.onlineSince)
	if err != nil {
		return nil, err
	}
	if r.Plan.BulkValue == 0 {
		return nil, domain.ErrUnlimitedData
	}
	bytes, errs := domain.ParseExtraBulk(value, unit)
	if len(errs) > 0 {
		return errs, nil
	}
	return nil, s.repo.SetExtraBulk(ctx, id, bytes, e.now)
}

// RevealPassword returns the decrypted member password (D8).
func (s *Service) RevealPassword(ctx context.Context, id int64) (string, error) {
	enc, err := s.repo.GetMemberPassword(ctx, id)
	if err != nil {
		return "", err
	}
	plain, err := s.cipher.Decrypt(enc)
	return string(plain), err
}

// SetPassword replaces the member password. A non-empty message means nothing was stored.
func (s *Service) SetPassword(ctx context.Context, id int64, password string) (string, error) {
	if _, err := s.repo.GetMember(ctx, id); err != nil {
		return "", err
	}
	if msg := domain.ValidateMemberPassword(password); msg != "" {
		return msg, nil
	}
	enc, err := s.cipher.Encrypt([]byte(password))
	if err != nil {
		return "", err
	}
	return "", s.repo.SetMemberPassword(ctx, id, enc, s.clock.Now())
}

// ListQuery is the member list request: filter plus paging.
type ListQuery struct {
	Filter    domain.MemberFilter
	Page, Per int
}

// ListResult is one page of the member list.
type ListResult struct {
	Rows                    []View
	Total, Page, Pages, Per int
	Loc                     *time.Location
}

// List returns one page of members with their summaries (BR-22).
func (s *Service) List(ctx context.Context, q ListQuery) (ListResult, error) {
	e, err := s.env(ctx)
	if err != nil {
		return ListResult{}, err
	}
	total, err := s.repo.CountFiltered(ctx, q.Filter, e.now) // the total decides the page clamp

	if err != nil {
		return ListResult{}, err
	}
	page, per, pages := domain.NormalizePaging(q.Page, q.Per, total)
	rows, err := s.repo.ListMembers(ctx, q.Filter, per, (page-1)*per, e.now, e.onlineSince)
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Total: total, Page: page, Pages: pages, Per: per, Loc: e.loc}
	for _, r := range rows {
		res.Rows = append(res.Rows, newView(r, e))
	}
	return res, nil
}

// BulkStats is what the delete confirmation shows.
type BulkStats struct{ Members, Online int }

// Stats counts the members of a selection and how many are online.
func (s *Service) Stats(ctx context.Context, sel domain.MemberSelection) (BulkStats, error) {
	if len(sel.IDs) > MaxSelectionIDs {
		return BulkStats{}, ErrSelectionTooLarge
	}
	e, err := s.env(ctx)
	if err != nil {
		return BulkStats{}, err
	}
	m, o, err := s.repo.SelectionStats(ctx, sel, e.now, e.onlineSince)
	return BulkStats{m, o}, err
}

// DeleteResult reports a delete: members removed and the kick outcome.
type DeleteResult struct {
	Deleted int
	Kick    KickResult
}

// MaxSelectionIDs is the most explicit ids one bulk action accepts (SQLite
// variable limit; the list pages hold at most 100). Filter selections are unbounded.
const MaxSelectionIDs = 500

// ErrSelectionTooLarge is returned for more than MaxSelectionIDs explicit ids.
var ErrSelectionTooLarge = errors.New("too many members selected")

// Delete kicks every open session of the selection (best effort), then deletes
// the members and their session rows in one transaction (E2.10, BR-23 style).
// A session opened after the kick step is seen inside that transaction and is
// kicked too, so no deleted member keeps a running session.
func (s *Service) Delete(ctx context.Context, sel domain.MemberSelection) (DeleteResult, error) {
	if len(sel.IDs) > MaxSelectionIDs {
		return DeleteResult{}, ErrSelectionTooLarge
	}
	now := s.clock.Now()
	kick, kicked, err := s.kick(ctx, sel, now)
	if err != nil {
		return DeleteResult{}, err
	}
	n, deleted, err := s.repo.DeleteMembers(ctx, sel, now)
	if err != nil {
		return DeleteResult{}, err
	}
	done := map[domain.SessionTarget]bool{}
	for _, t := range kicked {
		done[t] = true
	}
	for _, t := range deleted {
		if !done[t] {
			s.disconnect(ctx, []domain.SessionTarget{t}, &kick)
		}
	}
	return DeleteResult{Deleted: n, Kick: kick}, nil
}

// DeleteOne deletes a single member (domain.ErrNotFound if unknown).
func (s *Service) DeleteOne(ctx context.Context, id int64) (DeleteResult, error) {
	if _, err := s.repo.GetMember(ctx, id); err != nil {
		return DeleteResult{}, err
	}
	return s.Delete(ctx, domain.MemberSelection{IDs: []int64{id}})
}
