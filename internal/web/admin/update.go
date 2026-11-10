package admin

import (
	"net/http"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/update"
)

// updateView builds what the update page shows. The check time is shown in the settings time zone.
func (h handlers) updateView(r *http.Request, st update.Status) (UpdateProps, error) {
	cfg, err := h.Settings.Get(r.Context())
	if err != nil {
		return UpdateProps{}, err
	}
	loc, err := cfg.Location()
	if err != nil {
		return UpdateProps{}, err
	}
	p := UpdateProps{Status: st}
	if !st.CheckedAt.IsZero() {
		p.CheckedText = fmtTime(st.CheckedAt, loc) + " (" + domain.FormatRelative(h.now(), st.CheckedAt) + ")"
	}
	return p, nil
}

// updatePage shows the current and the latest version.
func (h handlers) updatePage(w http.ResponseWriter, r *http.Request) {
	st, err := h.Update.Status(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p, err := h.updateView(r, st)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, UpdatePage(layout(r, "Updates", "/admin/settings"), p))
}

// updateCheck asks GitHub now and answers with the fresh status block. With checks off it makes no call.
func (h handlers) updateCheck(w http.ResponseWriter, r *http.Request) {
	st, err := h.Update.Check(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	p, err := h.updateView(r, st)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	msg := "Checked"
	if st.Error != "" {
		msg = "The check failed"
	}
	setTrigger(w, map[string]any{"toast": msg})
	h.render(w, r, http.StatusOK, updateCheckAnswer(p))
}
