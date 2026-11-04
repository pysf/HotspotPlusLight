// Package voucher is the batch service: batch generation, the batch list,
// rename, delete and the data of the voucher print pages (BR-15, BR-16, BR-23).
package voucher

import (
	"context"
	"errors"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/secret"
)

// Repo is what the service needs from storage (store.Store). The Get methods return
// domain.ErrNotFound for an unknown id.
type Repo interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	// CreateBatch returns *domain.UsernameConflictError when a generated username exists.
	CreateBatch(ctx context.Context, nb domain.NewBatch, now time.Time) (domain.Batch, []string, error)
	GetBatch(ctx context.Context, id int64) (domain.Batch, error)
	CountBatches(ctx context.Context) (int, error)
	ListBatches(ctx context.Context, limit, offset int, onlineSince time.Time) ([]domain.BatchRow, error)
	RenameBatch(ctx context.Context, id int64, label string) error
	ListBatchCredentials(ctx context.Context, batchID int64) ([]domain.BatchCredential, error)
	GetMember(ctx context.Context, id int64) (domain.Member, error)
	GetMemberPassword(ctx context.Context, id int64) ([]byte, error)
}

// Cipher encrypts and decrypts member passwords (secret.Box).
type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

// Members deletes members the same way the member list does: it kicks open
// sessions first, then removes them (member.Service).
type Members interface {
	Delete(ctx context.Context, sel domain.MemberSelection) (member.DeleteResult, error)
}

// Service implements the batch and voucher use cases.
type Service struct {
	repo    Repo
	cipher  Cipher
	members Members
	clock   clock.Clock
}

// New returns a Service.
func New(repo Repo, cipher Cipher, members Members, clk clock.Clock) *Service {
	return &Service{repo: repo, cipher: cipher, members: members, clock: clk}
}

// NewForm returns the empty new-batch form. The default plan is only preselected (O10).
func (s *Service) NewForm(ctx context.Context) (domain.BatchForm, error) {
	st, err := s.repo.GetSettings(ctx)
	return domain.NewBatchForm(st.DefaultPlanID), err
}

// planChoiceMsg is the field error for a missing or unknown plan.
const planChoiceMsg = "Choose a plan from the list."

// Create validates the form and stores the batch (BR-15). A non-empty FieldErrors means
// nothing was stored. It returns *domain.UsernameConflictError, and stores nothing, when
// a generated username already exists.
func (s *Service) Create(ctx context.Context, f domain.BatchForm) (domain.Batch, domain.FieldErrors, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return domain.Batch{}, nil, err
	}
	loc, err := st.Location()
	if err != nil {
		return domain.Batch{}, nil, err
	}
	in, errs := domain.ParseBatchForm(f, st.BatchMaxCount)
	var plan domain.Plan
	if errs["plan_id"] == "" {
		plan, err = s.repo.GetPlan(ctx, in.PlanID)
		if errors.Is(err, domain.ErrNotFound) {
			errs["plan_id"] = planChoiceMsg
		} else if err != nil {
			return domain.Batch{}, nil, err
		}
	}
	if len(errs) > 0 {
		return domain.Batch{}, errs, nil
	}
	days := in.DurationDays
	if days == 0 { // zero means the plan's duration
		days = plan.DurationDays
	}
	now := s.clock.Now()
	pws := make([][]byte, in.Count)
	for i := range pws {
		pw, err := secret.RandomDigits(st.VoucherPasswordLength)
		if err != nil {
			return domain.Batch{}, nil, err
		}
		if pws[i], err = s.cipher.Encrypt([]byte(pw)); err != nil {
			return domain.Batch{}, nil, err
		}
	}
	b, _, err := s.repo.CreateBatch(ctx, domain.NewBatch{Input: in, DurationDays: days, ExpiresAt: domain.BatchExpiry(now, loc, days), PasswordsEnc: pws}, now)
	if errors.Is(err, domain.ErrPlanGone) { // deleted between the check and the insert
		return domain.Batch{}, domain.FieldErrors{"plan_id": planChoiceMsg}, nil
	}
	return b, nil, err
}

// ListResult is one page of the batch list.
type ListResult struct {
	Rows                    []domain.BatchRow
	Total, Page, Pages, Per int
	Loc                     *time.Location
}

// List returns one page of batches with their member, unused and online counts.
func (s *Service) List(ctx context.Context, page, per int) (ListResult, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return ListResult{}, err
	}
	loc, err := st.Location()
	if err != nil {
		return ListResult{}, err
	}
	total, err := s.repo.CountBatches(ctx)
	if err != nil {
		return ListResult{}, err
	}
	page, per, pages := domain.NormalizePaging(page, per, total)
	rows, err := s.repo.ListBatches(ctx, per, (page-1)*per, domain.ActiveSince(s.clock.Now(), st.AccountingIntervalSeconds))
	return ListResult{Rows: rows, Total: total, Page: page, Pages: pages, Per: per, Loc: loc}, err
}

// Rename changes the label only (BR-23a). It returns domain.ErrNotFound if the batch is unknown.
func (s *Service) Rename(ctx context.Context, id int64, label string) (domain.FieldErrors, error) {
	label, errs := domain.ParseBatchLabel(label)
	if len(errs) > 0 {
		if _, err := s.repo.GetBatch(ctx, id); err != nil {
			return nil, err
		}
		return errs, nil
	}
	return nil, s.repo.RenameBatch(ctx, id, label)
}

// Delete removes the batch with its members and their sessions (BR-23b), or returns
// domain.ErrNotFound. Open sessions are kicked first; a failed kick is counted, not fatal.
func (s *Service) Delete(ctx context.Context, id int64) (member.DeleteResult, error) {
	if _, err := s.repo.GetBatch(ctx, id); err != nil {
		return member.DeleteResult{}, err
	}
	return s.members.Delete(ctx, domain.MemberSelection{BatchID: &id})
}

// Print is the data of a voucher print page (BR-16).
type Print struct {
	Title string
	// BusinessName is printed on the top line of each voucher. Empty means no line.
	BusinessName string
	Help         string
	Pages        [][]domain.Voucher
	Total        int
}

// print builds the Print of vouchers vs with the current settings.
func (s *Service) print(ctx context.Context, title string, vs []domain.Voucher) (Print, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return Print{}, err
	}
	return Print{Title: title, BusinessName: st.BusinessName, Help: domain.VoucherHelp(st.VoucherHelp), Pages: domain.VoucherPages(vs), Total: len(vs)}, nil
}

// PrintBatch returns the vouchers of a batch with the current passwords, or domain.ErrNotFound.
func (s *Service) PrintBatch(ctx context.Context, id int64) (Print, error) {
	b, err := s.repo.GetBatch(ctx, id)
	if err != nil {
		return Print{}, err
	}
	creds, err := s.repo.ListBatchCredentials(ctx, id)
	if err != nil {
		return Print{}, err
	}
	vs := make([]domain.Voucher, len(creds))
	for i, c := range creds {
		plain, err := s.cipher.Decrypt(c.PasswordEnc)
		if err != nil {
			return Print{}, err
		}
		vs[i] = domain.Voucher{Username: c.Username, Password: string(plain)}
	}
	title := b.Label
	if title == "" {
		title = "Vouchers"
	}
	return s.print(ctx, title, vs)
}

// PrintMember returns the voucher of one member, in a batch or not, or domain.ErrNotFound.
func (s *Service) PrintMember(ctx context.Context, id int64) (Print, error) {
	m, err := s.repo.GetMember(ctx, id)
	if err != nil {
		return Print{}, err
	}
	enc, err := s.repo.GetMemberPassword(ctx, id)
	if err != nil {
		return Print{}, err
	}
	plain, err := s.cipher.Decrypt(enc)
	if err != nil {
		return Print{}, err
	}
	return s.print(ctx, "Voucher "+m.Username, []domain.Voucher{{Username: m.Username, Password: string(plain)}})
}
