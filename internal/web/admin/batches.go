package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/voucher"
	"hotspotpluslight/internal/web/ui"
)

const batchesHome = "/admin/batches"

// BatchService is implemented by voucher.Service.
type BatchService interface {
	NewForm(ctx context.Context) (domain.BatchForm, error)
	Create(ctx context.Context, f domain.BatchForm) (domain.Batch, domain.FieldErrors, error)
	List(ctx context.Context, page, per int) (voucher.ListResult, error)
	Rename(ctx context.Context, id int64, label string) (domain.FieldErrors, error)
	Delete(ctx context.Context, id int64) (member.DeleteResult, error)
	PrintBatch(ctx context.Context, id int64) (voucher.Print, error)
	PrintMember(ctx context.Context, id int64) (voucher.Print, error)
}

func batchURL(id int64) string { return fmt.Sprintf("%s/%d", batchesHome, id) }

func batchForm(r *http.Request) domain.BatchForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	return domain.BatchForm{Label: v("label"), PlanID: v("plan_id"), Count: v("count"), DurationDays: v("duration_days")}
}

func (h handlers) batches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := h.Batches.List(r.Context(), atoi(q.Get("page")), atoi(q.Get("per")))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, BatchesPage(layout(r, "Batches", batchesHome), res))
}

func (h handlers) batchFormProps(r *http.Request, f domain.BatchForm, errs domain.FieldErrors) (BatchFormProps, error) {
	plans, err := h.Plans.List(r.Context())
	return BatchFormProps{Form: f, Errors: errs, Plans: plans}, err
}

func (h handlers) batchNew(w http.ResponseWriter, r *http.Request) {
	f, err := h.Batches.NewForm(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p, err := h.batchFormProps(r, f, nil)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, BatchNewPage(layout(r, "New batch", batchesHome), p))
}

func (h handlers) batchCreate(w http.ResponseWriter, r *http.Request) {
	f := batchForm(r)
	b, errs, err := h.Batches.Create(r.Context(), f)
	var conflict *domain.UsernameConflictError
	if err != nil && !errors.As(err, &conflict) {
		h.serverError(w, r, err)
		return
	}
	if len(errs) > 0 || conflict != nil {
		p, perr := h.batchFormProps(r, f, errs)
		if perr != nil {
			h.serverError(w, r, perr)
			return
		}
		status := http.StatusUnprocessableEntity
		if conflict != nil {
			status = http.StatusConflict
			p.Conflict = fmt.Sprintf("Username %s is already taken, so no vouchers were created. Change the username prefix or the next voucher number in Settings.", conflict.Username)
		}
		h.render(w, r, status, BatchFormView(p))
		return
	}
	to := batchURL(b.ID) + "/vouchers"
	w.Header().Set("Location", to)
	htmxRedirect(w, r, http.StatusCreated, to)
}

func (h handlers) batchRename(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Batch")
		return
	}
	_ = r.ParseForm()
	errs, err := h.Batches.Rename(r.Context(), id, r.PostFormValue("label"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Batch")
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		h.render(w, r, http.StatusUnprocessableEntity, ui.FieldError("label", errs["label"]))
	default:
		h.render(w, r, http.StatusOK, ui.Flash("ok", "Label saved."))
	}
}

func (h handlers) batchDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Batch")
		return
	}
	res, err := h.Batches.Delete(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Batch")
	case err != nil:
		h.serverError(w, r, err)
	case res.Kick.Failed > 0:
		h.render(w, r, http.StatusOK, DeletedNotice(res, batchesHome, "Back to batches"))
	default:
		htmxRedirect(w, r, http.StatusOK, batchesHome)
	}
}

// batchPrint is the batch print / reprint page (BR-16).
func (h handlers) batchPrint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Batch")
		return
	}
	p, err := h.Batches.PrintBatch(r.Context(), id)
	h.printed(w, r, p, err, "Batch", batchesHome)
}

// memberPrint is the single-voucher page (BR-16, Q24).
func (h handlers) memberPrint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	p, err := h.Batches.PrintMember(r.Context(), id)
	h.printed(w, r, p, err, "Member", memberURL(id))
}

func (h handlers) printed(w http.ResponseWriter, r *http.Request, p voucher.Print, err error, what, back string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, what)
	case err != nil:
		h.serverError(w, r, err)
	default:
		h.render(w, r, http.StatusOK, VoucherSheets(p, back))
	}
}

// batchesHref builds the link to a page of the batch list.
func batchesHref(per int) func(int) string {
	return func(page int) string {
		return batchesHome + "?page=" + strconv.Itoa(page) + "&per=" + strconv.Itoa(per)
	}
}
