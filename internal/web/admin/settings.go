package admin

import (
	"net/http"

	"hotspotpluslight/internal/domain"
)

// settingsForm reads the settings form from the request. A server field that is
// absent from the request (an old page or a script) keeps its current value from cur.
func settingsForm(r *http.Request, cur domain.Settings) domain.SettingsForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	keep := domain.FormFromSettings(cur)
	server := func(name, current string) string {
		if !r.PostForm.Has(name) {
			return current
		}
		return v(name)
	}
	return domain.SettingsForm{
		BusinessName: v("business_name"), Timezone: v("timezone"), RadiusSharedSecret: v("radius_shared_secret"),
		CoaPort: v("coa_port"), AccountingIntervalSeconds: v("accounting_interval_seconds"), DefaultPlanID: v("default_plan_id"),
		BatchUsernamePrefix: v("batch_username_prefix"), BatchCounter: v("batch_counter"), BatchMaxCount: v("batch_max_count"),
		VoucherPasswordLength: v("voucher_password_length"), VoucherHelp: v("voucher_help"),
		SessionRetentionDays: v("session_retention_days"),
		AdminAddr:            server("admin_addr", keep.AdminAddr), RadiusAuthAddr: server("radius_auth_addr", keep.RadiusAuthAddr),
		RadiusAcctAddr: server("radius_acct_addr", keep.RadiusAcctAddr),
		LogLevel:       server("log_level", keep.LogLevel), LogFormat: server("log_format", keep.LogFormat),
		CheckUpdates: server("check_updates", keep.CheckUpdates),
	}
}

// restartNeeded reports whether the saved server settings differ from the running ones.
func (h handlers) restartNeeded(saved domain.Settings) bool {
	return h.RunningServer != nil && domain.RestartNeeded(saved.Server(), h.RunningServer())
}

// settingsPage shows the settings form.
func (h handlers) settingsPage(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	plans, err := h.Plans.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, SettingsPage(layout(r, "Settings", "/admin/settings"), SettingsProps{Form: domain.FormFromSettings(st), Plans: plans, RestartNeeded: h.restartNeeded(st)}))
}

// settingsUpdate saves the settings, or shows the field errors with 422.
func (h handlers) settingsUpdate(w http.ResponseWriter, r *http.Request) {
	cur, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	f := settingsForm(r, cur)
	errs, err := h.Settings.Update(r.Context(), f)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	plans, err := h.Plans.List(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if len(errs) > 0 {
		h.render(w, r, http.StatusUnprocessableEntity, SettingsFormView(SettingsProps{Form: f, Errors: errs, Plans: plans, RestartNeeded: h.restartNeeded(cur)}))
		return
	}
	saved, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	setTrigger(w, map[string]any{"toast": "Settings saved"})
	h.render(w, r, http.StatusOK, SettingsFormView(SettingsProps{Form: f, Plans: plans, Saved: true, RestartNeeded: h.restartNeeded(saved)}))
}
