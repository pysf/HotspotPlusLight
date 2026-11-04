package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/web/ui"
)

// plansHome is the path of the plans page.
const plansHome = "/admin/plans"

// planForm reads the plan form from the request.
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

// planID returns the plan id of the path. It returns false for anything that is not a positive integer.
func planID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

// planURL returns the path of one plan.
func planURL(id int64) string { return fmt.Sprintf("%s/%d", plansHome, id) }

// notFound answers 404 for a missing what (for example "Member"). In a list or sheet it is a toast.
func (h handlers) notFound(w http.ResponseWriter, r *http.Request, what string) {
	// The row in a list or sheet is gone: refresh the list and say so in a toast.
	if changed := map[string]string{"Member": membersChanged, "Plan": plansChanged}[what]; changed != "" && fromApp(r) {
		ev := map[string]any{changed: true, "toast": "This " + strings.ToLower(what) + " no longer exists."}
		if inSheet(r) {
			ev["close-sheet"] = true
		}
		setTrigger(w, ev)
		w.Header().Set("HX-Reswap", "none")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h.render(w, r, http.StatusNotFound, ui.ErrorFragment(what+" not found."))
}

// plansList shows the plans, or only the list region when htmx asks for it.
func (h handlers) plansList(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Plans.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if hxTarget(r) == planListID {
		h.render(w, r, http.StatusOK, PlanListFragment(plans))
		return
	}
	h.render(w, r, http.StatusOK, PlansPage(layout(r, "Plans", plansHome), plans))
}

// planNew shows the new plan form, in the sheet or as a page.
func (h handlers) planNew(w http.ResponseWriter, r *http.Request) {
	if inSheet(r) {
		h.render(w, r, http.StatusOK, PlanFormView(PlanFormProps{Form: domain.NewPlanForm(), Action: "post", URL: plansHome, Sheet: true}))
		return
	}
	h.render(w, r, http.StatusOK, PlanNewPage(layout(r, "New plan", plansHome)))
}

// planCreate creates a plan, or shows the field errors with 422.
func (h handlers) planCreate(w http.ResponseWriter, r *http.Request) {
	f := planForm(r)
	id, errs, err := h.Plans.Create(r.Context(), f)
	switch {
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		h.render(w, r, http.StatusUnprocessableEntity, PlanFormView(PlanFormProps{Form: f, Errors: errs, Action: "post", URL: plansHome, Sheet: inSheet(r)}))
	default:
		w.Header().Set("Location", planURL(id))
		if inSheet(r) {
			h.appDone(w, r, http.StatusCreated, plansChanged, "Plan created.")
			return
		}
		htmxRedirect(w, r, http.StatusCreated, plansHome)
	}
}

// planEdit shows the edit plan form, in the sheet or as a page.
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
	case inSheet(r):
		h.render(w, r, http.StatusOK, PlanFormView(PlanFormProps{Form: domain.FormFromPlan(d.Plan), Action: "put", URL: planURL(id), Members: d.Members, Sheet: true}))
	default:
		h.render(w, r, http.StatusOK, PlanEditPage(layout(r, "Edit plan", plansHome),
			PlanFormProps{Form: domain.FormFromPlan(d.Plan), Action: "put", URL: planURL(id), Members: d.Members}))
	}
}

// planUpdate saves a plan, or shows the field errors with 422.
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
		h.render(w, r, http.StatusUnprocessableEntity, PlanFormView(PlanFormProps{Form: f, Errors: errs, Action: "put", URL: planURL(id), Members: d.Members, Sheet: inSheet(r)}))
	case inSheet(r):
		h.appDone(w, r, http.StatusOK, plansChanged, "Plan saved.")
	default:
		htmxRedirect(w, r, http.StatusOK, plansHome)
	}
}

// planDelete deletes a plan (O12). A plan in use or the default plan answers 409.
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
		msg := fmt.Sprintf("This plan is used by %d members and cannot be deleted.", inUse.Members)
		if inUse.Members == 0 {
			msg = fmt.Sprintf("This plan is used by %s and cannot be deleted. Delete the batches first.", plural(inUse.Batches, "batch"))
		}
		h.appOrFlash(w, r, http.StatusConflict, msg)
	case errors.Is(err, domain.ErrDefault):
		h.appOrFlash(w, r, http.StatusConflict, "This plan is the default plan. Clear the default plan in Settings first.")
	case err != nil:
		h.serverError(w, r, err)
	case fromApp(r):
		h.appDone(w, r, http.StatusOK, plansChanged, "Plan deleted.")
	default:
		htmxRedirect(w, r, http.StatusOK, plansHome)
	}
}
