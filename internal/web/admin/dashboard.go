package admin

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"hotspotpluslight/internal/dashboard"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/linkstatus"
	"hotspotpluslight/internal/web/ui"
)

// DashboardService is the read-only numbers of internal/dashboard.
type DashboardService interface {
	Overview(ctx context.Context) (dashboard.Overview, error)
}

// dashboardHome is the path of the dashboard, the first page after login.
const dashboardHome = "/admin"

// dashboardContentURL is the fragment that the whole dashboard polls.
const dashboardContentURL = "/admin/dashboard/content"

// dashboardView is the dashboard prepared for display.
type dashboardView struct {
	Online    dashboard.OnlineNow
	Traffic   trafficTile
	Failed    failedTile
	Link      linkView
	Expiring  attentionView
	NearLimit attentionView
	Charts    []chartView
}

// trafficTile is the Traffic today tile as text.
type trafficTile struct {
	Today                   string
	Yesterday, LastWeek     string // sizes
	VsYesterday, VsLastWeek string // "+12%", "-5%" or "no data"
	TodayBytes              int64
}

// failedTile is the Failed logins today tile as text.
type failedTile struct {
	Total  int64
	HasTop bool
	Code   string // the top reason code
	Text   string // the English text of the top reason
	Count  int64
}

// attentionView is one Needs attention list.
type attentionView struct {
	Key, Title, Column, Empty string
	Count                     int
	Rows                      []attentionRow
}

// attentionRow is one member of an attention list.
type attentionRow struct {
	ID       int64
	Username string
	Detail   string
}

// chartView is one chart with its heading and the link under it.
type chartView struct {
	Key, Title, Caption string
	Bars                []ui.Bar
	Top                 int64
	Empty               string // shown instead of the chart when every bar is 0
	More                string // link text to a longer report ("" = none)
	MoreHref            string
}

// changeText renders a percent change; a base of 0 has no percent (BR-26).
func changeText(c dashboard.Change) string {
	if !c.Known {
		return "no data"
	}
	if c.Percent > 0 {
		return "+" + strconv.Itoa(c.Percent) + "%"
	}
	return strconv.Itoa(c.Percent) + "%"
}

// topOf returns the largest bar value.
func topOf(bars []ui.Bar) int64 {
	var top int64
	for _, b := range bars {
		top = max(top, b.Value)
	}
	return top
}

// trafficBars maps the zero-filled days to chart columns (the same hooks as the usage page).
func trafficBars(days []domain.UsageDay) []ui.Bar {
	bars := make([]ui.Bar, len(days))
	for i, d := range days {
		bars[i] = ui.Bar{
			Label: d.Day,
			Value: d.Total(),
			Tip:   d.Day + ": " + domain.FormatSize(d.Down) + " down, " + domain.FormatSize(d.Up) + " up",
			Attrs: templ.Attributes{"data-bar": true, "data-day": d.Day, "data-down": strconv.FormatInt(d.Down, 10), "data-up": strconv.FormatInt(d.Up, 10)},
		}
	}
	return bars
}

// activeBars maps the daily active members to chart columns.
func activeBars(days []domain.DayCount) []ui.Bar {
	bars := make([]ui.Bar, len(days))
	for i, d := range days {
		bars[i] = ui.Bar{
			Label: d.Day,
			Value: int64(d.Count),
			Tip:   d.Day + ": " + strconv.Itoa(d.Count) + " members",
			Attrs: templ.Attributes{"data-bar": true, "data-day": d.Day, "data-count": strconv.Itoa(d.Count)},
		}
	}
	return bars
}

// busyBars maps the 24 hourly averages to chart columns. A bar is a whole number, so its height is the average in
// hundredths, and a non-zero average is never lower than 1 so it always draws. data-avg carries two decimals.
func busyBars(avg [24]float64) []ui.Bar {
	bars := make([]ui.Bar, 24)
	for h, v := range avg {
		hour := fmt.Sprintf("%02d:00", h)
		height := int64(math.Round(v * 100))
		if v > 0 {
			height = max(height, 1)
		}
		bars[h] = ui.Bar{
			Label: hour,
			Value: height,
			Tip:   hour + ": " + strconv.FormatFloat(v, 'f', 1, 64) + " online on average",
			Attrs: templ.Attributes{"data-bar": true, "data-hour": strconv.Itoa(h), "data-avg": strconv.FormatFloat(v, 'f', 2, 64)},
		}
	}
	return bars
}

// busyPeak returns the largest hourly average as text with one decimal. It reads the averages, not the rounded bar heights.
func busyPeak(avg [24]float64) string {
	var top float64
	for _, v := range avg {
		top = max(top, v)
	}
	return strconv.FormatFloat(top, 'f', 1, 64)
}

// capPeak is a chart caption that ends with the peak value.
func capPeak(what, peak string) string { return what + "; peak " + peak }

// newDashboardView prepares the overview and the link status snapshot for display.
func newDashboardView(o dashboard.Overview, snap linkstatus.Snapshot) dashboardView {
	v := dashboardView{
		Online: o.Online,
		Traffic: trafficTile{
			Today: domain.FormatSize(o.Traffic.Today), Yesterday: domain.FormatSize(o.Traffic.Yesterday), LastWeek: domain.FormatSize(o.Traffic.LastWeek),
			VsYesterday: changeText(o.Traffic.VsYesterday), VsLastWeek: changeText(o.Traffic.VsLastWeek),
			TodayBytes: o.Traffic.Today,
		},
		Failed: failedTile{Total: o.Failures.Total, HasTop: o.Failures.HasTop},
		Link:   linkRows(snap, o.Loc),
	}
	if o.Failures.HasTop {
		v.Failed.Code, v.Failed.Text, v.Failed.Count = string(o.Failures.Top.Reason), o.Failures.Top.Reason.Message(), o.Failures.Top.Count
	}
	days := strconv.Itoa(domain.ExpiringDays)
	v.Expiring = attentionView{Key: "expiring", Title: "Expiring in " + days + " days", Column: "Expires", Empty: "No member expires in the next " + days + " days.", Count: o.Expiring.Count}
	for _, r := range o.Expiring.Rows {
		v.Expiring.Rows = append(v.Expiring.Rows, attentionRow{ID: r.MemberID, Username: r.Username,
			Detail: r.ExpiresAt.In(o.Loc).Format("2006-01-02") + " (" + domain.FormatRelative(o.Now, r.ExpiresAt) + ")"})
	}
	v.NearLimit = attentionView{Key: "near-limit", Title: "Near the data limit", Column: "Data used", Empty: "No member is near the data limit.", Count: o.NearLimit.Count}
	for _, r := range o.NearLimit.Rows {
		d := strconv.Itoa(r.Percent) + "% used"
		if r.OutOfData {
			d = "Out of data"
		}
		v.NearLimit.Rows = append(v.NearLimit.Rows, attentionRow{ID: r.MemberID, Username: r.Username, Detail: d})
	}
	traffic := trafficBars(o.TrafficDays)
	busy := busyBars(o.BusyHours)
	active := activeBars(o.ActiveDays)
	n := strconv.Itoa(len(o.TrafficDays))
	v.Charts = []chartView{
		{Key: "traffic", Title: "Traffic, last " + n + " days", Bars: traffic, Top: topOf(traffic), Empty: "No traffic recorded yet.",
			Caption: capPeak("download + upload", domain.FormatSize(topOf(traffic))), More: "Usage details", MoreHref: usageHome},
		{Key: "busy-hours", Title: "Busy hours", Bars: busy, Top: topOf(busy), Empty: "No sessions recorded yet.",
			Caption: capPeak("average people online, by hour, last "+n+" days", busyPeak(o.BusyHours))},
		{Key: "active-members", Title: "Active members per day", Bars: active, Top: topOf(active), Empty: "No member used the internet yet.",
			Caption: capPeak("members with traffic", strconv.FormatInt(topOf(active), 10))},
	}
	return v
}

// dashboardView builds the whole dashboard for one request.
func (h handlers) dashboardView(r *http.Request) (dashboardView, error) {
	o, err := h.Dashboard.Overview(r.Context())
	if err != nil {
		return dashboardView{}, err
	}
	return newDashboardView(o, h.Link.Snapshot()), nil
}

// dashboardPage shows the home page.
func (h handlers) dashboardPage(w http.ResponseWriter, r *http.Request) {
	v, err := h.dashboardView(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, DashboardPage(layout(r, "Dashboard", dashboardHome), v))
}

// dashboardContent shows everything under the page title as a fragment. The dashboard polls it every 10 seconds.
func (h handlers) dashboardContent(w http.ResponseWriter, r *http.Request) {
	v, err := h.dashboardView(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, DashboardContent(v, true))
}

// linkEvent is one row of the link status table.
type linkEvent struct{ Key, Label, At, From, Detail string }

// badSource is one row of the bad-secret table.
type badSource struct{ From, Count, At string }

// linkView is the link status prepared for display.
type linkView struct {
	Events     []linkEvent
	BadTotal   string
	BadSources []badSource
}

// linkTimeFormat is the time format of the link status.
const linkTimeFormat = "2006-01-02 15:04:05"

// linkRows returns the link status in snapshot s with times in loc.
func linkRows(s linkstatus.Snapshot, loc *time.Location) linkView {
	ev := func(key, label string, e linkstatus.Event) linkEvent {
		if e.At.IsZero() {
			return linkEvent{Key: key, Label: label, At: "never"}
		}
		return linkEvent{key, label, e.At.In(loc).Format(linkTimeFormat), e.From, e.Detail}
	}
	v := linkView{
		Events: []linkEvent{
			ev("access-request", "Last Access-Request", s.AccessRequest),
			ev("accounting", "Last Accounting packet", s.Accounting),
			ev("disconnect", "Last Disconnect", s.Disconnect),
		},
		BadTotal: strconv.Itoa(s.BadSecretTotal),
	}
	for _, b := range s.BadSecretSources {
		v.BadSources = append(v.BadSources, badSource{b.From, strconv.Itoa(b.Count), b.Last.In(loc).Format(linkTimeFormat)})
	}
	return v
}
