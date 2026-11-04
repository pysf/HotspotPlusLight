package admin

import (
	"net/http"

	"hotspotpluslight/internal/domain"
)

// settingsForm reads the settings form from the request.
func settingsForm(r *http.Request) domain.SettingsForm {
	_ = r.ParseForm()
	v := r.PostFormValue
	return domain.SettingsForm{
		BusinessName: v("business_name"), Timezone: v("timezone"), RadiusSharedSecret: v("radius_shared_secret"),
		CoaPort: v("coa_port"), AccountingIntervalSeconds: v("accounting_interval_seconds"), DefaultPlanID: v("default_plan_id"),
		BatchUsernamePrefix: v("batch_username_prefix"), BatchCounter: v("batch_counter"), BatchMaxCount: v("batch_max_count"),
		VoucherPasswordLength: v("voucher_password_length"), VoucherHelp: v("voucher_help"),
		SessionRetentionDays: v("session_retention_days"),
	}
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
	h.render(w, r, http.StatusOK, SettingsPage(layout(r, "Settings", "/admin/settings"), SettingsProps{Form: domain.FormFromSettings(st), Plans: plans}))
}

// settingsUpdate saves the settings, or shows the field errors with 422.
func (h handlers) settingsUpdate(w http.ResponseWriter, r *http.Request) {
	f := settingsForm(r)
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
		h.render(w, r, http.StatusUnprocessableEntity, SettingsFormView(SettingsProps{Form: f, Errors: errs, Plans: plans}))
		return
	}
	h.render(w, r, http.StatusOK, SettingsFormView(SettingsProps{Form: f, Plans: plans, Saved: true}))
}
