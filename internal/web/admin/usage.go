package admin

import (
	"context"
	"net/http"
	"strconv"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/usage"
	"hotspotpluslight/internal/web/ui"
)

// UsageService is the usage report part of internal/usage.
type UsageService interface {
	Report(ctx context.Context, rangeDays int) (usage.View, error)
}

// usageHome is the path of the usage page.
const usageHome = "/admin/usage"

// usageHref returns the link to the usage page for a range of days.
func usageHref(days int) string { return usageHome + "?days=" + strconv.Itoa(days) }

// usageBars maps the daily totals to chart columns; the data attributes are the test hooks.
func usageBars(v usage.View) []ui.Bar {
	bars := make([]ui.Bar, len(v.Days))
	for i, d := range v.Days {
		bars[i] = ui.Bar{
			Label: d.Day,
			Value: d.Total(),
			Tip:   d.Day + ": " + domain.FormatSize(d.Down) + " down, " + domain.FormatSize(d.Up) + " up",
			Attrs: map[string]any{"data-bar": true, "data-day": d.Day, "data-down": strconv.FormatInt(d.Down, 10), "data-up": strconv.FormatInt(d.Up, 10)},
		}
	}
	return bars
}

// usagePage shows the usage history.
func (h handlers) usagePage(w http.ResponseWriter, r *http.Request) {
	v, err := h.Usage.Report(r.Context(), atoi(r.URL.Query().Get("days")))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, UsagePage(layout(r, "Usage", usageHome), v))
}
