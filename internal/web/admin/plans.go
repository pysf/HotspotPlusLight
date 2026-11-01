package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/web/ui"
)

const plansHome = "/admin/plans"

func planForm(r *http.Request) domain.PlanForm {
	_ = r.ParseForm()
	return domain.PlanForm{
		Name: r.PostFormValue("name"), DurationDays: r.PostFormValue("duration_days"),
		SpeedValue: r.PostFormValue("speed_value"), SpeedUnit: r.PostFormValue("speed_unit"),
		BulkValue: r.PostFormValue("bulk_value"), BulkUnit: r.PostFormValue("bulk_unit"),
		TimeLimitMinutes: r.PostFormValue("time_limit_minutes"), MaxSessions: r.PostFormValue("max_sessions"),
		AutoResubscribe: domain.Checked(r.PostFormValue("auto_resubscribe")),
	}
}

// planID parses {id}; anything that is not a positive integer is an unknown plan (404).
func planID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func planURL(id int64) string { return fmt.Sprintf("%s/%d", plansHome, id) }

func (h handlers) notFound(w http.ResponseWriter, r *http.Request, what string) {
	h.render(w, r, http.StatusNotFound, ui.ErrorFragment(what+" not found."))
}

func (h handlers) plansList(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Plans.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, PlansPage(layout(r, "Plans", plansHome), plans))
}

func (h handlers) planNew(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, PlanNewPage(layout(r, "New plan", plansHome)))
}

func (h handlers) planCreate(w http.ResponseWriter, r *http.Request) {
	f := planForm(r)
	id, errs, err := h.Plans.Create(r.Context(), f)
	switch {
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		h.render(w, r, http.StatusUnprocessableEntity, PlanFormView(PlanFormProps{Form: f, Errors: errs, Action: "post", URL: plansHome}))
	default:
		w.Header().Set("Location", planURL(id))
		htmxRedirect(w, r, http.StatusCreated, plansHome)
	}
}

func (h handlers) planEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		h.notFound(w, r, "Plan")
		return
	}
	d, err := h.Plans.Get(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Plan")
	case err != nil:
		h.serverError(w, r, err)
	default:
		h.render(w, r, http.StatusOK, PlanEditPage(layout(r, "Edit plan", plansHome),
			PlanFormProps{Form: domain.FormFromPlan(d.Plan), Action: "put", URL: planURL(id), Members: d.Members}))
	}
}

func (h handlers) planUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		h.notFound(w, r, "Plan")
		return
	}
	f := planForm(r)
	errs, err := h.Plans.Update(r.Context(), id, f)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Plan")
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		d, err := h.Plans.Get(r.Context(), id) // the member warning stays on the re-rendered form
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		h.render(w, r, http.StatusUnprocessableEntity, PlanFormView(PlanFormProps{Form: f, Errors: errs, Action: "put", URL: planURL(id), Members: d.Members}))
	default:
		htmxRedirect(w, r, http.StatusOK, plansHome)
	}
}

func (h handlers) planDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := planID(r)
	if !ok {
		h.notFound(w, r, "Plan")
		return
	}
	err := h.Plans.Delete(r.Context(), id)
	var inUse *domain.InUseError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Plan")
	case errors.As(err, &inUse):
		h.render(w, r, http.StatusConflict, ui.ErrorFragment(fmt.Sprintf("This plan is used by %d members and cannot be deleted.", inUse.Members)))
	case errors.Is(err, domain.ErrDefault):
		h.render(w, r, http.StatusConflict, ui.ErrorFragment("This plan is the default plan. Clear the default plan in Settings first."))
	case err != nil:
		h.serverError(w, r, err)
	default:
		htmxRedirect(w, r, http.StatusOK, plansHome)
	}
}
