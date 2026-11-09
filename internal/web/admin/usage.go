package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

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

// dayTime parses a usage day key. A bad key gives the zero time, which never happens for keys the report makes.
func dayTime(key string) time.Time {
	t, _ := time.Parse(domain.DayFormat, key)
	return t
}

// dayShort prints a day key as "6 Nov".
func dayShort(key string) string { return dayTime(key).Format("2 Jan") }

// dayLong prints a day key as "Friday 6 November".
func dayLong(key string) string { return dayTime(key).Format("Monday 2 January") }

// usageBars maps the daily totals to chart columns; the data attributes are the test hooks.
func usageBars(v usage.View) []ui.Bar {
	bars := make([]ui.Bar, len(v.Days))
	for i, d := range v.Days {
		bars[i] = ui.Bar{
			Label: dayShort(d.Day),
			Value: d.Total(),
			Tip:   dayTime(d.Day).Format("Mon 2 Jan") + ": " + domain.FormatSize(d.Down) + " down, " + domain.FormatSize(d.Up) + " up",
			Attrs: map[string]any{"data-bar": true, "data-day": d.Day, "data-down": strconv.FormatInt(d.Down, 10), "data-up": strconv.FormatInt(d.Up, 10)},
		}
	}
	return bars
}

// usagePeriod returns the first and last day of the range, such as "11 Oct to 9 Nov".
func usagePeriod(v usage.View) string {
	if len(v.Days) == 0 {
		return ""
	}
	return dayShort(v.Days[0].Day) + " to " + dayShort(v.Days[len(v.Days)-1].Day)
}

// usageChartLabel is the accessible description of the daily chart.
func usageChartLabel(v usage.View) string {
	s := "Traffic per day over the last " + strconv.Itoa(v.Range) + " days. Average " + domain.FormatSize(v.Summary.Average) + "."
	if v.Summary.PeakDay != "" {
		s += " The peak was " + domain.FormatSize(v.Summary.PeakBytes) + " on " + dayLong(v.Summary.PeakDay) + "."
	}
	return s
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
