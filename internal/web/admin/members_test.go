package admin

import (
	"testing"
	"time"

	"hotspotpluslight/internal/domain"
)

func TestPlanSummary(t *testing.T) {
	cases := []struct {
		name string
		p    domain.Plan
		want string
	}{
		{"full", domain.Plan{BulkValue: 10, BulkUnit: "GB", DurationDays: 30, SpeedValue: 10, SpeedUnit: "Mbps", MaxSessions: 3}, "10 GB · 30 days · 10 Mbps · 3 devices"},
		{"singular", domain.Plan{BulkValue: 1.5, BulkUnit: "GB", DurationDays: 1, SpeedValue: 512, SpeedUnit: "Kbps", MaxSessions: 1}, "1.5 GB · 1 day · 512 Kbps · 1 device"},
		{"unlimited", domain.Plan{DurationDays: 7, MaxSessions: 2}, "Unlimited data · 7 days · Unlimited speed · 2 devices"},
		{"time limit", domain.Plan{BulkValue: 1, BulkUnit: "GB", DurationDays: 2, SpeedValue: 2, SpeedUnit: "Mbps", MaxSessions: 1, TimeLimitMinutes: 120}, "1 GB · 2 days · 2 Mbps · 2 hours online · 1 device"},
	}
	for _, c := range cases {
		if got := planSummary(c.p); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMemberSince(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, loc)
	if got := memberSince(time.Date(2026, 10, 14, 8, 0, 0, 0, loc), now, loc); got != "14 Oct" {
		t.Errorf("same year: %q", got)
	}
	if got := memberSince(time.Date(2025, 3, 2, 8, 0, 0, 0, loc), now, loc); got != "2 Mar 2025" {
		t.Errorf("other year: %q", got)
	}
}
