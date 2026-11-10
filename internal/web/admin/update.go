package admin

import (
	"errors"
	"net/http"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/update"
	"hotspotpluslight/internal/web/ui"
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
	p := UpdateProps{Status: st, Progress: h.Installer.Progress()}
	if st.Available {
		plan := h.Installer.Plan()
		p.InstallOK, p.InstallReason = plan.OK, plan.Reason
	}
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

// updateProgress answers the polled progress block.
func (h handlers) updateProgress(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, UpdateProgressView(h.Installer.Progress()))
}

// updateInstall starts the install as a background job and answers at once with the progress block.
// A second install while one runs answers 409; a refused install (setting off, dev build, no file for this
// system, program file not writable) answers 422. Both set a toast and leave the block as it is.
func (h handlers) updateInstall(w http.ResponseWriter, r *http.Request) {
	err := h.Installer.Start(r.Context())
	var refused *update.RefusedError
	switch {
	case errors.Is(err, update.ErrBusy):
		h.updateRefuse(w, r, http.StatusConflict, "An update is already running.")
	case errors.As(err, &refused):
		h.updateRefuse(w, r, http.StatusUnprocessableEntity, refused.Reason)
	case err != nil:
		h.serverError(w, r, err)
	default:
		setTrigger(w, map[string]any{"toast": "Update started"})
		h.render(w, r, http.StatusOK, UpdateProgressView(h.Installer.Progress()))
	}
}

// updateRefuse answers a refused install with a toast and the reason as text. htmx does not swap it into the page.
func (h handlers) updateRefuse(w http.ResponseWriter, r *http.Request, status int, msg string) {
	setTrigger(w, map[string]any{"toast": msg})
	w.Header().Set("HX-Reswap", "none")
	h.render(w, r, status, ui.Flash("warn", msg))
}
