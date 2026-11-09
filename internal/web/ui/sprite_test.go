package ui

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
)

// Every icon name that the shell and the shared components use must exist in the sprite.
func TestSpriteHasEveryIconInUse(t *testing.T) {
	names := []string{"home", "users", "wifi", "layers", "ticket", "chart", "router", "settings", "key", "sun", "moon", "logout", "more",
		"check-circle", "clock", "alert", "slash", "info", "search", "plus", "x", "copy", "chevron-down", "chevron-left", "chevron-right", "trash"}
	for _, n := range names {
		if !strings.Contains(spriteSymbols, `id="i-`+n+`"`) {
			t.Errorf("icon %q is not in the sprite", n)
		}
	}
	for _, tone := range []string{"ok", "warn", "bad", "off", "info"} {
		if icon := pillIcon(tone); !strings.Contains(spriteSymbols, `id="i-`+icon+`"`) {
			t.Errorf("pill tone %q uses icon %q, which is not in the sprite", tone, icon)
		}
	}
}

func TestPillToneMapsMemberStatuses(t *testing.T) {
	for status, want := range map[string]string{"active": "ok", "expiring": "warn", "expired": "bad", "disabled": "off", "info": "info", "other": "off"} {
		if got := pillTone(status); got != want {
			t.Errorf("pillTone(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestParseThemeFallsBackToAuto(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "light": "light", "dark": "dark", "air": "auto", `"><script>`: "auto"} {
		if got := ParseTheme(in); got != want {
			t.Errorf("ParseTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func render(t *testing.T, c templ.Component) *goquery.Document {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	d, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The toast holds one status icon (check mark); toast.js turns it into the alert icon for an error.
func TestToastHasOneStatusIcon(t *testing.T) {
	d := render(t, Toast())
	if n := d.Find("#toast .c-toast__icon").Length(); n != 1 {
		t.Fatalf("%d status icons, want 1", n)
	}
	if href := d.Find("#toast .c-toast__icon use").AttrOr("href", ""); href != "#i-check-circle" {
		t.Errorf("status icon %q", href)
	}
	if d.Find("#toast[role=status] [data-toast-text]").Length() != 1 || d.Find("#toast [data-toast-close]").Length() != 1 {
		t.Error("toast lacks its text or close button")
	}
}

// The pager sits inside the table card, and only when there is more than one page.
func TestTablePagedPutsPagerInsideCard(t *testing.T) {
	href := func(n int) string { return "?page=" + strconv.Itoa(n) }
	d := render(t, TablePaged("Sample", []string{"A"}, PageProps{Page: 1, Pages: 3, Href: href, Total: 30, Per: 10}))
	if d.Find(`.c-card[data-flush] .c-pager nav[aria-label="Pages"]`).Length() != 1 {
		t.Error("pager is not inside the card")
	}
	if got := strings.TrimSpace(d.Find("[data-pager-summary]").Text()); got != "1–10 of 30" {
		t.Errorf("summary %q", got)
	}
	one := render(t, TablePaged("Sample", []string{"A"}, PageProps{Page: 1, Pages: 1, Href: href}))
	if one.Find(".c-pager").Length() != 0 {
		t.Error("a single page must have no pager")
	}
}

// 80 % is already a warning: the meter's low limit sits just under 80 % of the maximum.
func TestMeterWarnsAtEightyPercent(t *testing.T) {
	d := render(t, Meter(8, 10, "8 of 10 GB"))
	low, err := strconv.ParseFloat(d.Find("meter").AttrOr("low", ""), 64)
	if err != nil || low >= 8 || low < 7.99 {
		t.Errorf("low = %v (%v), want just under 8", low, err)
	}
}

func TestOnlineBadge(t *testing.T) {
	d := render(t, OnlineBadge(0, true))
	b := d.Find("#online-badge")
	if _, hidden := b.Attr("hidden"); !hidden || b.AttrOr("hx-swap-oob", "") != "true" {
		t.Error("a zero badge must be hidden and sent out of band")
	}
	b = render(t, OnlineBadge(3, false)).Find("#online-badge")
	if _, hidden := b.Attr("hidden"); hidden || strings.TrimSpace(b.Text()) != "3" || b.AttrOr("hx-swap-oob", "") != "" {
		t.Error("a badge with 3 online must show 3 and not be out of band")
	}
}
