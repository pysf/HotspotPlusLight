package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/web/ui"
)

// membersHome is the path of the members page.
const membersHome = "/admin/members"

// memberURL returns the path of one member.
func memberURL(id int64) string { return fmt.Sprintf("%s/%d", membersHome, id) }

// pathID returns the {id} of the path. It returns false for 0 or an overflow; the route matches digits only.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

// memberForm reads the member form from the request.
func memberForm(r *http.Request) domain.MemberForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	return domain.MemberForm{
		Username: v("username"), Password: v("password"), FirstName: v("first_name"), LastName: v("last_name"),
		Mobile: v("mobile"), Email: v("email"), NationalCode: v("national_code"), PlanID: v("plan_id"),
		MAC: v("mac"), ExpiresAt: v("expires_at"), Active: domain.Checked(v("active")),
	}
}

// filterFrom returns the list filter read through get, from a query string or a form.
func filterFrom(get func(string) string) domain.MemberFilter {
	plan, _ := strconv.ParseInt(get("plan"), 10, 64)
	f := domain.MemberFilter{Query: get("q"), PlanID: max(plan, 0)}
	switch s := get("status"); s {
	case domain.StatusActive, domain.StatusExpiring, domain.StatusInactive, domain.StatusExpired:
		f.Status = s
	}
	return f
}

// atoi returns the number in s, or 0 if there is none.
func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// members shows the members list, or only the list region when htmx asks for it.
func (h handlers) members(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := filterFrom(q.Get)
	res, err := h.Members.List(r.Context(), member.ListQuery{Filter: filter, Page: atoi(q.Get("page")), Per: atoi(q.Get("per"))})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	props := MembersProps{Result: res, Filter: filter, URL: r.URL.RequestURI()}
	// Only the list region asks for a fragment (HX-Target); the harness sends HX-Request on every request.
	if hxTarget(r) == listID {
		h.render(w, r, http.StatusOK, MemberListFragment(props))
		return
	}
	if props.Plans, err = h.Plans.List(r.Context()); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, MembersPage(layout(r, "Members", membersHome), props))
}

// formProps returns the props of the member form (new or edit).
func (h handlers) formProps(r *http.Request, f domain.MemberForm, errs domain.FieldErrors) (MemberFormProps, error) {
	plans, err := h.Plans.List(r.Context())
	return MemberFormProps{Form: f, Errors: errs, Plans: plans}, err
}

// memberNew shows the new member form, in the sheet or as a page.
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
	if inSheet(r) {
		p.Sheet = true
		h.render(w, r, http.StatusOK, MemberNewSheet(p))
		return
	}
	h.render(w, r, http.StatusOK, MemberNewPage(layout(r, "New member", membersHome), p))
}

// memberCreate creates a member, or shows the field errors with 422.
func (h handlers) memberCreate(w http.ResponseWriter, r *http.Request) {
	f := memberForm(r)
	id, errs, err := h.Members.Create(r.Context(), f)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	sheet := inSheet(r)
	if len(errs) > 0 {
		p, err := h.formProps(r, f, errs)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		p.Create, p.URL, p.Sheet = true, membersHome, sheet
		h.render(w, r, http.StatusUnprocessableEntity, MemberFormView(p))
		return
	}
	w.Header().Set("Location", memberURL(id))
	if !sheet {
		htmxRedirect(w, r, http.StatusCreated, memberURL(id))
		return
	}
	// In the sheet the answer is the credentials card, and the sheet stays open until "Done".
	card, err := h.createdCard(r, id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setTrigger(w, map[string]any{membersChanged: true, "toast": "Member created."})
	h.render(w, r, http.StatusCreated, MemberCreated(card))
}

// createdCard returns what the credentials card shows. The password may be a generated one.
func (h handlers) createdCard(r *http.Request, id int64) (createdVM, error) {
	v, err := h.Members.Get(r.Context(), id)
	if err != nil {
		return createdVM{}, err
	}
	pw, err := h.Members.RevealPassword(r.Context(), id)
	if err != nil {
		return createdVM{}, err
	}
	vm := newVM(v)
	return createdVM{Username: vm.Username, Password: pw, Plan: vm.PlanName, Expires: vm.ExpiresFull}, nil
}

// memberPage shows one member, in the sheet or as a page.
func (h handlers) memberPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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
	if inSheet(r) { // the edit sheet is this same resource (GET /admin/members/{id}) as a fragment
		p.Sheet = true
		h.render(w, r, http.StatusOK, MemberEditSheet(p, newVM(v)))
		return
	}
	h.render(w, r, http.StatusOK, MemberPage(layout(r, v.Row.Member.Username, membersHome), newVM(v), p))
}

// memberUpdate saves a member. If some sessions could not be disconnected (BR-18), it says so.
func (h handlers) memberUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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
	sheet := inSheet(r)
	if len(errs) == 0 && kick.Failed == 0 {
		if sheet {
			h.appDone(w, r, http.StatusOK, membersChanged, "Member saved.")
			return
		}
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
		return
	}
	p, err := h.formProps(r, f, errs)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p.URL, p.Sheet = memberURL(id), sheet
	status := http.StatusOK
	if len(errs) > 0 {
		status = http.StatusUnprocessableEntity
	} else {
		// Saved, but some sessions could not be disconnected (BR-18): tell the admin.
		p.Warning = fmt.Sprintf("Saved. %d of %d open sessions could not be disconnected.", kick.Failed, kick.Attempted)
		if sheet {
			setTrigger(w, map[string]any{membersChanged: true})
		}
	}
	h.renderMemberForm(w, r, status, id, p)
}

// renderMemberForm answers with the member form. In the sheet it is the whole edit sheet.
func (h handlers) renderMemberForm(w http.ResponseWriter, r *http.Request, status int, id int64, p MemberFormProps) {
	if !p.Sheet {
		h.render(w, r, status, MemberFormView(p))
		return
	}
	v, err := h.Members.Get(r.Context(), id)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, status, MemberEditSheet(p, newVM(v)))
}

// memberToggle sets a member to the state in the form value active (1 or 0). The target is
// explicit, so a double click does the same twice. Disabling disconnects the open sessions.
func (h handlers) memberToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	var active bool
	switch r.PostFormValue("active") {
	case "1":
		active = true
	case "0":
	default:
		h.appOrFlash(w, r, http.StatusBadRequest, "Missing target state.")
		return
	}
	kick, err := h.Members.SetActive(r.Context(), id, active)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
		return
	case err != nil:
		h.serverError(w, r, err)
		return
	}
	msg := "Member enabled."
	if !active {
		msg = "Member disabled."
	}
	if kick.Failed > 0 { // same wording as the edit form (BR-18)
		msg = fmt.Sprintf("%s %d of %d open sessions could not be disconnected.", msg, kick.Failed, kick.Attempted)
		if !fromApp(r) {
			h.render(w, r, http.StatusOK, ui.Flash("warn", msg))
			return
		}
	}
	if fromApp(r) {
		h.appDone(w, r, http.StatusOK, membersChanged, msg)
		return
	}
	htmxRedirect(w, r, http.StatusOK, memberURL(id))
}

// firstError returns the first field error in field name order, or "" if there is none.
func firstError(errs domain.FieldErrors) string {
	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return errs[keys[0]]
}

// memberDelete deletes one member and disconnects its open sessions (BR-18).
func (h handlers) memberDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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
	case fromApp(r):
		msg := "Member deleted."
		if res.Kick.Failed > 0 {
			msg = fmt.Sprintf("Member deleted. %d of %d open sessions could not be disconnected.", res.Kick.Failed, res.Kick.Attempted)
		}
		h.appDone(w, r, http.StatusOK, membersChanged, msg)
	case res.Kick.Failed > 0:
		h.render(w, r, http.StatusOK, DeletedNotice(res, membersHome, "Back to members"))
	default:
		htmxRedirect(w, r, http.StatusOK, membersHome)
	}
}

// memberRenew renews a member by hand (BR-13).
func (h handlers) memberRenew(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	switch err := h.Members.Renew(r.Context(), id); {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	case fromApp(r):
		h.appDone(w, r, http.StatusOK, membersChanged, "Member renewed.")
	default:
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
	}
}

// memberExtraBulk sets the extra bulk of a member. A plan with unlimited data answers 409.
func (h handlers) memberExtraBulk(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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

// passwordReveal shows the stored password of a member.
func (h handlers) passwordReveal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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

// passwordSet sets a new password for a member.
func (h handlers) passwordSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
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

// bulkSelection returns the delete selection: every result of the filter, or the ticked ids.
// It returns false when nothing is selected.
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
		h.render(w, r, http.StatusOK, DeletedNotice(res, membersHome, "Back to members"))
	case hxTarget(r) != "": // from the page: refresh the list, chips and pager; the empty body clears the confirm box
		setTrigger(w, map[string]any{membersChanged: true, "toast": "Members deleted."})
		w.WriteHeader(http.StatusOK)
	default:
		htmxRedirect(w, r, http.StatusOK, membersHome)
	}
}

// listURL returns the link to the list with the filter and page size. Page 0 leaves the page out.
func listURL(f domain.MemberFilter, per, page int) string {
	v := url.Values{"per": {strconv.Itoa(per)}}
	if page > 0 {
		v.Set("page", strconv.Itoa(page))
	}
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

// listHref returns a function that gives the link to a page of the list, keeping the filter.
func listHref(f domain.MemberFilter, per int) func(int) string {
	return func(page int) string { return listURL(f, per, page) }
}

// memberVM is a member prepared for display. The list rows and the member page share it (BR-22).
type memberVM struct {
	ID                          int64
	Username, Name, Mobile, MAC string
	PlanName                    string
	Status                      string
	Data, Time, WindowEnd       string
	DataMain, DataLeft          string // list cell: "used / allowed" and the muted "N left" line
	Active                      bool
	PlanSpeed                   string
	HasMeter                    bool // false on an unlimited plan
	MeterUsed, MeterMax         float64
	Expires, ExpiresRel         string // list cell: the date and "in 2 days"; "—" and "" when there is none
	ExpiresFull                 string // date and time, or "No expiry"
	VerdictCode, VerdictText    string
	Online                      bool
	ExtraBulk                   string // in the plan's unit, e.g. "1.5 GB"
	ExtraBulkValue              string
	PlanUnit                    string
	UnlimitedData               bool
	RenewalText                 string
	Member                      domain.Member
}

// fmtTime returns t in loc as date and time.
func fmtTime(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01-02 15:04") }

// newVM returns the display values of a member view.
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
	vm.Active, vm.PlanSpeed = m.Active, limit(p.SpeedValue, p.SpeedUnit)
	if !s.Quota.DataUnlimited && s.Quota.AllowedBytes > 0 {
		vm.HasMeter, vm.MeterUsed, vm.MeterMax = true, float64(s.Quota.UsedBytes), float64(s.Quota.AllowedBytes)
	}
	vm.Expires, vm.ExpiresFull = "—", "No expiry"
	if m.ExpiresAt != nil {
		vm.Expires = m.ExpiresAt.In(v.Loc).Format("2006-01-02")
		vm.ExpiresRel = domain.FormatRelative(v.Now, *m.ExpiresAt)
		vm.ExpiresFull = fmtTime(*m.ExpiresAt, v.Loc)
	}
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

// trimJoin returns a and b joined by a space, leaving out an empty one.
func trimJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}
