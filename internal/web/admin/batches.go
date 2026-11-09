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

// batchesHome is the path of the batches page.
const batchesHome = "/admin/batches"

// BatchService is the batch and voucher part of internal/voucher.
type BatchService interface {
	NewForm(ctx context.Context) (domain.BatchForm, error)
	Create(ctx context.Context, f domain.BatchForm) (domain.Batch, domain.FieldErrors, error)
	List(ctx context.Context, page, per int) (voucher.ListResult, error)
	Rename(ctx context.Context, id int64, label string) (domain.FieldErrors, error)
	Delete(ctx context.Context, id int64) (member.DeleteResult, error)
	PrintBatch(ctx context.Context, id int64) (voucher.Print, error)
	PrintMember(ctx context.Context, id int64) (voucher.Print, error)
}

// batchURL returns the path of one batch.
func batchURL(id int64) string { return fmt.Sprintf("%s/%d", batchesHome, id) }

// batchForm reads the batch form from the request.
func batchForm(r *http.Request) domain.BatchForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	return domain.BatchForm{Label: v("label"), PlanID: v("plan_id"), Count: v("count"), DurationDays: v("duration_days")}
}

// batches shows the batch list.
func (h handlers) batches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := h.Batches.List(r.Context(), atoi(q.Get("page")), atoi(q.Get("per")))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, BatchesPage(layout(r, "Vouchers", batchesHome), res))
}

// batchFormProps returns the props of the batch form.
func (h handlers) batchFormProps(r *http.Request, f domain.BatchForm, errs domain.FieldErrors) (BatchFormProps, error) {
	plans, err := h.Plans.List(r.Context())
	return BatchFormProps{Form: f, Errors: errs, Plans: plans}, err
}

// batchNew shows the new batch form.
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

// batchCreate creates a batch and goes to its voucher page. A taken username answers 409.
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

// batchRename changes the label of a batch.
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

// batchDelete deletes a batch with its members and disconnects their open sessions (BR-18).
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

// batchPrint shows the print page of a batch (BR-16).
func (h handlers) batchPrint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Batch")
		return
	}
	p, err := h.Batches.PrintBatch(r.Context(), id)
	h.printed(w, r, p, err, "Batch", batchesHome)
}

// memberPrint shows the print page of one member's voucher (BR-16, Q24).
func (h handlers) memberPrint(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	p, err := h.Batches.PrintMember(r.Context(), id)
	h.printed(w, r, p, err, "Member", memberURL(id))
}

// printed shows the voucher sheets for p, or the error for err. back is the link of the back button.
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

// batchesHref returns a function that gives the link to a page of the batch list.
func batchesHref(per int) func(int) string {
	return func(page int) string {
		return batchesHome + "?page=" + strconv.Itoa(page) + "&per=" + strconv.Itoa(per)
	}
}
