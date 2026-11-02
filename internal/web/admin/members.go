package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/web/ui"
)

const membersHome = "/admin/members"

func memberURL(id int64) string { return fmt.Sprintf("%s/%d", membersHome, id) }

// memberID parses {id}; the route only matches digits, so this fails only for 0 or overflow (404).
func memberID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func memberForm(r *http.Request) domain.MemberForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	return domain.MemberForm{
		Username: v("username"), Password: v("password"), FirstName: v("first_name"), LastName: v("last_name"),
		Mobile: v("mobile"), Email: v("email"), NationalCode: v("national_code"), PlanID: v("plan_id"),
		MAC: v("mac"), ExpiresAt: v("expires_at"), Active: domain.Checked(v("active")),
	}
}

// filterFrom reads the list filter from a query string or a form.
func filterFrom(get func(string) string) domain.MemberFilter {
	plan, _ := strconv.ParseInt(get("plan"), 10, 64)
	f := domain.MemberFilter{Query: get("q"), PlanID: max(plan, 0)}
	switch s := get("status"); s {
	case domain.StatusActive, domain.StatusInactive, domain.StatusExpired:
		f.Status = s
	}
	return f
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func (h handlers) members(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := filterFrom(q.Get)
	res, err := h.Members.List(r.Context(), member.ListQuery{Filter: filter, Page: atoi(q.Get("page")), Per: atoi(q.Get("per"))})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	plans, err := h.Plans.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, MembersPage(layout(r, "Members", membersHome), MembersProps{Result: res, Filter: filter, Plans: plans}))
}

// formProps builds the props of the member form (new or edit).
func (h handlers) formProps(r *http.Request, f domain.MemberForm, errs domain.FieldErrors) (MemberFormProps, error) {
	plans, err := h.Plans.List(r.Context())
	return MemberFormProps{Form: f, Errors: errs, Plans: plans}, err
}

func (h handlers) memberNew(w http.ResponseWriter, r *http.Request) {
	f, err := h.Members.NewForm(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p, err := h.formProps(r, f, nil)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p.Create, p.URL = true, membersHome
	h.render(w, r, http.StatusOK, MemberNewPage(layout(r, "New member", membersHome), p))
}

func (h handlers) memberCreate(w http.ResponseWriter, r *http.Request) {
	f := memberForm(r)
	id, errs, err := h.Members.Create(r.Context(), f)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if len(errs) > 0 {
		p, err := h.formProps(r, f, errs)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		p.Create, p.URL = true, membersHome
		h.render(w, r, http.StatusUnprocessableEntity, MemberFormView(p))
		return
	}
	w.Header().Set("Location", memberURL(id))
	htmxRedirect(w, r, http.StatusCreated, memberURL(id))
}

func (h handlers) memberPage(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	v, err := h.Members.Get(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		h.notFound(w, r, "Member")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p, err := h.formProps(r, domain.FormFromMember(v.Row.Member, v.Loc), nil)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p.URL = memberURL(id)
	h.render(w, r, http.StatusOK, MemberPage(layout(r, v.Row.Member.Username, membersHome), newVM(v), p))
}

func (h handlers) memberUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	f := memberForm(r)
	errs, kick, err := h.Members.Update(r.Context(), id, f)
	if errors.Is(err, domain.ErrNotFound) {
		h.notFound(w, r, "Member")
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if len(errs) == 0 && kick.Failed == 0 {
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
		return
	}
	p, err := h.formProps(r, f, errs)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p.URL = memberURL(id)
	if len(errs) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity, MemberFormView(p))
		return
	}
	// Saved, but some sessions could not be disconnected (BR-18): tell the admin.
	p.Warning = fmt.Sprintf("Saved. %d of %d open sessions could not be disconnected.", kick.Failed, kick.Attempted)
	h.render(w, r, http.StatusOK, MemberFormView(p))
}

func (h handlers) memberDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	res, err := h.Members.DeleteOne(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	case res.Kick.Failed > 0:
		h.render(w, r, http.StatusOK, DeletedNotice(res))
	default:
		htmxRedirect(w, r, http.StatusOK, membersHome)
	}
}

func (h handlers) memberRenew(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	switch err := h.Members.Renew(r.Context(), id); {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	default:
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
	}
}

func (h handlers) memberExtraBulk(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	_ = r.ParseForm()
	value, unit := r.PostFormValue("value"), r.PostFormValue("unit")
	errs, err := h.Members.SetExtraBulk(r.Context(), id, value, unit)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case errors.Is(err, domain.ErrUnlimitedData):
		h.render(w, r, http.StatusConflict, ui.ErrorFragment("This plan has unlimited data, so extra bulk cannot be added."))
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		h.render(w, r, http.StatusUnprocessableEntity, ExtraBulkFormView(ExtraBulkProps{URL: memberURL(id) + "/extra-bulk", Value: value, Unit: unit, Errors: errs}))
	default:
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
	}
}

func (h handlers) passwordReveal(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	pw, err := h.Members.RevealPassword(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	default:
		h.render(w, r, http.StatusOK, PasswordShown(pw))
	}
}

func (h handlers) passwordSet(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	_ = r.ParseForm()
	msg, err := h.Members.SetPassword(r.Context(), id, r.PostFormValue("password"))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	case msg != "":
		h.render(w, r, http.StatusUnprocessableEntity, ui.ErrorFragment("Password: "+msg))
	default:
		h.render(w, r, http.StatusOK, ui.Flash("ok", "Password changed."))
	}
}

// bulkSelection reads the delete selection: every result of the filter, or the ticked ids.
func bulkSelection(r *http.Request) (domain.MemberSelection, bool) {
	_ = r.ParseForm()
	if domain.Checked(r.PostFormValue("all_results")) {
		f := filterFrom(r.PostFormValue)
		return domain.MemberSelection{Filter: &f}, true
	}
	var ids []int64
	for _, s := range r.PostForm["id"] {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return domain.MemberSelection{IDs: ids}, len(ids) > 0
}

// memberBulkDelete is two steps on one route: without confirmed=1 it answers the
// confirmation (counts); with it, it deletes (Q26, Q29).
func (h handlers) memberBulkDelete(w http.ResponseWriter, r *http.Request) {
	sel, ok := bulkSelection(r)
	if !ok {
		h.render(w, r, http.StatusUnprocessableEntity, ui.ErrorFragment("No member selected."))
		return
	}
	if r.PostFormValue("confirmed") != "1" {
		st, err := h.Members.Stats(r.Context(), sel)
		if errors.Is(err, member.ErrSelectionTooLarge) {
			h.render(w, r, http.StatusUnprocessableEntity, ui.ErrorFragment("Too many members selected: at most 500 at once."))
			return
		}
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		h.render(w, r, http.StatusOK, BulkConfirm(st, r.PostForm))
		return
	}
	res, err := h.Members.Delete(r.Context(), sel)
	switch {
	case errors.Is(err, member.ErrSelectionTooLarge):
		h.render(w, r, http.StatusUnprocessableEntity, ui.ErrorFragment("Too many members selected: at most 500 at once."))
	case err != nil:
		h.serverError(w, r, err)
	case res.Kick.Failed > 0:
		h.render(w, r, http.StatusOK, DeletedNotice(res))
	default:
		htmxRedirect(w, r, http.StatusOK, membersHome)
	}
}

// listHref builds the link to a page of the list, keeping the filters.
func listHref(f domain.MemberFilter, per int) func(int) string {
	return func(page int) string {
		v := url.Values{"page": {strconv.Itoa(page)}, "per": {strconv.Itoa(per)}}
		if f.Query != "" {
			v.Set("q", f.Query)
		}
		if f.PlanID > 0 {
			v.Set("plan", strconv.FormatInt(f.PlanID, 10))
		}
		if f.Status != "" {
			v.Set("status", f.Status)
		}
		return membersHome + "?" + v.Encode()
	}
}

// memberVM is a member prepared for display: one place that turns the domain
// summary into text, shared by the list rows and the member page (BR-22).
type memberVM struct {
	ID                          int64
	Username, Name, Mobile, MAC string
	PlanName                    string
	Status                      string
	Data, Time, WindowEnd       string
	DataMain, DataLeft          string // list cell: "used / allowed" and the muted "N left" line
	VerdictCode, VerdictText    string
	Online                      bool
	ExtraBulk                   string // in the plan's unit, e.g. "1.5 GB"
	ExtraBulkValue              string
	PlanUnit                    string
	UnlimitedData               bool
	RenewalText                 string
	Member                      domain.Member
}

func fmtTime(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01-02 15:04") }

func newVM(v member.View) memberVM {
	m, p, s := v.Row.Member, v.Row.Plan, v.Summary
	vm := memberVM{
		ID: m.ID, Username: m.Username, Mobile: m.Mobile, MAC: m.MAC, PlanName: p.Name, Status: s.Status, Online: v.Row.Online,
		Name:        trimJoin(m.FirstName, m.LastName),
		Data:        s.Quota.DataText(p.BulkUnit),
		Time:        s.Quota.TimeText(),
		WindowEnd:   fmtTime(s.Quota.WindowEnd, v.Loc),
		VerdictCode: string(s.Decision.Reason), VerdictText: domain.ReasonLabel(s.Decision.Reason),
		ExtraBulk: domain.FormatBytes(m.ExtraBulkBytes, p.BulkUnit), ExtraBulkValue: domain.BytesIn(m.ExtraBulkBytes, p.BulkUnit),
		PlanUnit: p.BulkUnit, UnlimitedData: p.BulkValue == 0, Member: m,
	}
	vm.DataMain, vm.DataLeft = s.Quota.DataParts(p.BulkUnit)
	if s.Decision.Allowed() && s.Decision.Renew {
		vm.VerdictText = "renews at next login"
	}
	if m.RenewalCount > 0 {
		kind := map[string]string{"auto": "automatic", "manual": "manual"}[m.LastRenewalKind]
		times := "times"
		if m.RenewalCount == 1 {
			times = "time"
		}
		last := ""
		if m.LastRenewedAt != nil {
			last = ", last on " + fmtTime(*m.LastRenewedAt, v.Loc)
		}
		vm.RenewalText = fmt.Sprintf("Renewed %d %s%s (%s)", m.RenewalCount, times, last, kind)
	}
	return vm
}

func trimJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
