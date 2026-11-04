// Package member is the member service: create, edit, renew, extra bulk,
// passwords, list and delete. Deleting or deactivating a member kicks its open
// sessions first.
package member

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/secret"
)

// Repo is what the service needs from storage (store.Store). Create and Update return
// domain.ErrUsernameTaken on a case-insensitive duplicate. Get, Update, Set and Renew
// return domain.ErrNotFound for an unknown id.
type Repo interface {
	CreateMember(ctx context.Context, in domain.MemberInput, passwordEnc []byte, now time.Time) (int64, error)
	GetMember(ctx context.Context, id int64) (domain.Member, error)
	GetMemberRow(ctx context.Context, id int64, onlineSince time.Time) (domain.MemberRow, error)
	UpdateMember(ctx context.Context, id int64, in domain.MemberInput, now time.Time) error
	SetMemberActive(ctx context.Context, id int64, active bool, now time.Time) error
	GetMemberPassword(ctx context.Context, id int64) ([]byte, error)
	SetMemberPassword(ctx context.Context, id int64, passwordEnc []byte, now time.Time) error
	RenewMember(ctx context.Context, id int64, kind string, now time.Time) error
	SetExtraBulk(ctx context.Context, id int64, bytes int64, now time.Time) error
	CountFiltered(ctx context.Context, f domain.MemberFilter, now time.Time) (int, error)
	// CountStatuses counts per status under the query and plan of f (its Status is ignored).
	CountStatuses(ctx context.Context, f domain.MemberFilter, now time.Time) (domain.StatusCounts, error)
	ListMembers(ctx context.Context, f domain.MemberFilter, limit, offset int, now, onlineSince time.Time) ([]domain.MemberRow, error)
	SelectionStats(ctx context.Context, sel domain.MemberSelection, now, onlineSince time.Time) (members, online int, err error)
	OpenSessions(ctx context.Context, sel domain.MemberSelection, now time.Time) ([]domain.SessionTarget, error)
	// DeleteMembers returns how many members it deleted and the open sessions
	// they had inside the same transaction.
	DeleteMembers(ctx context.Context, sel domain.MemberSelection, now time.Time) (int, []domain.SessionTarget, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	GetSettings(ctx context.Context) (domain.Settings, error)
}

// Cipher encrypts member passwords at rest (secret.Box).
type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

// Disconnector ends a router session (BR-18). session.Service sends the RADIUS Disconnect and marks the row ended on ACK.
type Disconnector interface {
	Disconnect(ctx context.Context, t domain.SessionTarget) error
}

// Service implements the member use cases.
type Service struct {
	repo   Repo
	cipher Cipher
	disc   Disconnector
	clock  clock.Clock
}

// New returns a Service.
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

// env loads the settings and the clock for one request.
func (s *Service) env(ctx context.Context) (env, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return env{}, err
	}
	loc, err := st.Location()
	if err != nil {
		return env{}, err
	}
	now := s.clock.Now()
	return env{st, loc, now, domain.ActiveSince(now, st.AccountingIntervalSeconds)}, nil
}

// View is a member with everything the admin pages show about it.
type View struct {
	Row     domain.MemberRow
	Summary domain.Summary
	Loc     *time.Location
	Now     time.Time
}

// newView builds the View of a member row.
func newView(r domain.MemberRow, e env) View {
	return View{Row: r, Summary: domain.Summarize(r.Member, r.Plan, e.now), Loc: e.loc, Now: e.now}
}

// NewForm returns the form for a new member, active by default. The default plan is only preselected, never assigned (O10, Q19).
func (s *Service) NewForm(ctx context.Context) (domain.MemberForm, error) {
	st, err := s.repo.GetSettings(ctx)
	return domain.NewMemberForm(st.DefaultPlanID), err
}

// Location returns the display zone (settings.timezone).
func (s *Service) Location(ctx context.Context) (*time.Location, error) {
	e, err := s.env(ctx)
	return e.loc, err
}

// planChoiceMsg is the field error for a missing or unknown plan.
const planChoiceMsg = "Choose a plan from the list."

// planError returns a field error when the chosen plan does not exist, else nil.
func (s *Service) planError(ctx context.Context, planID int64) (domain.FieldErrors, error) {
	if _, err := s.repo.GetPlan(ctx, planID); errors.Is(err, domain.ErrNotFound) {
		return domain.FieldErrors{"plan_id": planChoiceMsg}, nil
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

// Create validates and stores a member (BR-14) and returns its id. It makes a random
// 6-digit password when none is given. A non-empty FieldErrors means nothing was stored.
func (s *Service) Create(ctx context.Context, f domain.MemberForm) (int64, domain.FieldErrors, error) {
	e, err := s.env(ctx)
	if err != nil {
		return 0, nil, err
	}
	in, errs := domain.ParseMember(f, e.loc, true)
	password := f.Password
	if strings.TrimSpace(password) == "" {
		if password, err = secret.RandomDigits(6); err != nil {
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
	if errors.Is(err, domain.ErrPlanGone) { // deleted between the check and the insert
		return 0, domain.FieldErrors{"plan_id": planChoiceMsg}, nil
	}
	return id, nil, err
}

// Get returns one member with its summary, or domain.ErrNotFound.
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

// kick sends a Disconnect for every open session of the selection and returns the
// outcome and the targets. A failed Disconnect is counted, not an error (BR-18, BR-23).
func (s *Service) kick(ctx context.Context, sel domain.MemberSelection, now time.Time) (KickResult, []domain.SessionTarget, error) {
	targets, err := s.repo.OpenSessions(ctx, sel, now)
	if err != nil {
		return KickResult{}, nil, err
	}
	var res KickResult
	s.disconnect(ctx, targets, &res)
	return res, targets, nil
}

// kickWorkers is the most Disconnects one request sends at a time. Each may wait up
// to 3 s for a silent router, so serial sends would stall the HTTP request.
const kickWorkers = 8

// disconnect sends a Disconnect for each target, at most kickWorkers at a time, and
// adds the outcome to res.
func (s *Service) disconnect(ctx context.Context, targets []domain.SessionTarget, res *KickResult) {
	failed := make([]bool, len(targets)) // one slot per target: no shared writes
	sem := make(chan struct{}, kickWorkers)
	var wg sync.WaitGroup
	for i, t := range targets {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			failed[i] = s.disc.Disconnect(ctx, t) != nil
		}()
	}
	wg.Wait()
	for _, f := range failed {
		res.Attempted++
		if f {
			res.Failed++
		}
	}
}

// DisconnectMember kicks every open session of one member (BR-18, E5.07).
// It returns domain.ErrNotFound if the member does not exist.
func (s *Service) DisconnectMember(ctx context.Context, id int64) (KickResult, error) {
	if _, err := s.repo.GetMember(ctx, id); err != nil {
		return KickResult{}, err
	}
	res, _, err := s.kick(ctx, domain.MemberSelection{IDs: []int64{id}}, s.clock.Now())
	return res, err
}

// Update validates and stores the member's editable fields. Changing the plan only
// changes plan_id (BR-13a). Deactivating kicks the open sessions, and the member stays
// inactive whatever the kick result (BR-18, Q14). A non-empty FieldErrors means nothing
// was stored; renaming an online member is refused.
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
	// A past expires_at needs no kick here: session.Enforcer disconnects the member at the next Interim.
	err = s.repo.UpdateMember(ctx, id, in, e.now)
	if errors.Is(err, domain.ErrUsernameTaken) {
		return domain.FieldErrors{"username": "This username is already taken."}, KickResult{}, nil
	}
	if errors.Is(err, domain.ErrPlanGone) { // deleted between the check and the update
		return domain.FieldErrors{"plan_id": planChoiceMsg}, KickResult{}, nil
	}
	if err != nil {
		return nil, KickResult{}, err
	}
	kick, err := s.kickIfDeactivated(ctx, id, before.Active, in.Active, e.now)
	return nil, kick, err
}

// kickIfDeactivated kicks the open sessions only when the member went from active (was) to inactive (now) (BR-18).
func (s *Service) kickIfDeactivated(ctx context.Context, id int64, was, now bool, at time.Time) (KickResult, error) {
	if !was || now {
		return KickResult{}, nil
	}
	kick, _, err := s.kick(ctx, domain.MemberSelection{IDs: []int64{id}}, at)
	return kick, err
}

// SetActive sets the active flag and nothing else. Deactivating an active member kicks the
// open sessions like Update does. Setting the state it already has does nothing.
func (s *Service) SetActive(ctx context.Context, id int64, active bool) (KickResult, error) {
	before, err := s.repo.GetMember(ctx, id)
	if err != nil {
		return KickResult{}, err
	}
	if before.Active == active {
		return KickResult{}, nil
	}
	now := s.clock.Now()
	if err := s.repo.SetMemberActive(ctx, id, active, now); err != nil {
		return KickResult{}, err
	}
	return s.kickIfDeactivated(ctx, id, before.Active, active, now)
}

// Renew restarts the member's validity window by hand (BR-13b, kind "manual").
func (s *Service) Renew(ctx context.Context, id int64) error {
	return s.repo.RenewMember(ctx, id, "manual", s.clock.Now())
}

// SetExtraBulk sets (not adds) the member's extra bulk (Q17). It returns domain.ErrNotFound,
// or domain.ErrUnlimitedData when the plan has unlimited data. Bad input comes back as FieldErrors.
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

// RevealPassword returns the member's decrypted password (D8), or domain.ErrNotFound.
func (s *Service) RevealPassword(ctx context.Context, id int64) (string, error) {
	enc, err := s.repo.GetMemberPassword(ctx, id)
	if err != nil {
		return "", err
	}
	plain, err := s.cipher.Decrypt(enc)
	return string(plain), err
}

// SetPassword replaces the member's password. A non-empty message means the password was invalid and nothing was stored.
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

// ListQuery is a member list request: a filter and the paging.
type ListQuery struct {
	Filter    domain.MemberFilter
	Page, Per int
}

// ListResult is one page of the member list.
type ListResult struct {
	Rows                    []View
	Total, Page, Pages, Per int
	Counts                  domain.StatusCounts // ignores the filter's status
	Loc                     *time.Location
}

// List returns one page of members with their summaries (BR-22). The page is clamped to the last page.
func (s *Service) List(ctx context.Context, q ListQuery) (ListResult, error) {
	e, err := s.env(ctx)
	if err != nil {
		return ListResult{}, err
	}
	total, err := s.repo.CountFiltered(ctx, q.Filter, e.now)
	if err != nil {
		return ListResult{}, err
	}
	page, per, pages := domain.NormalizePaging(q.Page, q.Per, total)
	rows, err := s.repo.ListMembers(ctx, q.Filter, per, (page-1)*per, e.now, e.onlineSince)
	if err != nil {
		return ListResult{}, err
	}
	counts, err := s.repo.CountStatuses(ctx, q.Filter, e.now)
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Total: total, Page: page, Pages: pages, Per: per, Counts: counts, Loc: e.loc}
	for _, r := range rows {
		res.Rows = append(res.Rows, newView(r, e))
	}
	return res, nil
}

// BulkStats is what the delete confirmation shows.
type BulkStats struct{ Members, Online int }

// Stats returns how many members a selection holds and how many are online.
// It returns ErrSelectionTooLarge for too many explicit ids.
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

// MaxSelectionIDs is the most explicit ids one bulk action accepts. It keeps the
// number of SQL variables in the IN list low. Filter selections have no limit.
const MaxSelectionIDs = 500

// ErrSelectionTooLarge is returned for more than MaxSelectionIDs explicit ids.
var ErrSelectionTooLarge = errors.New("too many members selected")

// Delete kicks every open session of the selection (best effort), then deletes the
// members and their sessions in one transaction (E2.10). A session that opened after the
// first kick is seen inside that transaction and is kicked too. It returns
// ErrSelectionTooLarge for too many explicit ids.
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
	var missed []domain.SessionTarget
	for _, t := range deleted {
		if !done[t] {
			missed = append(missed, t)
		}
	}
	s.disconnect(ctx, missed, &kick)
	return DeleteResult{Deleted: n, Kick: kick}, nil
}

// DeleteOne deletes a single member, or returns domain.ErrNotFound.
func (s *Service) DeleteOne(ctx context.Context, id int64) (DeleteResult, error) {
	if _, err := s.repo.GetMember(ctx, id); err != nil {
		return DeleteResult{}, err
	}
	return s.Delete(ctx, domain.MemberSelection{IDs: []int64{id}})
}
