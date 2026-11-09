package admin

import (
	"testing"

	"hotspotpluslight/internal/plan"
)

func TestBlockedReason(t *testing.T) {
	cases := []struct {
		d    plan.Detail
		want string
	}{
		{plan.Detail{}, ""},
		{plan.Detail{Members: 1}, "1 member uses this plan. Move them to another plan first."},
		{plan.Detail{Members: 37}, "37 members use this plan. Move them to another plan first."},
		{plan.Detail{Batches: 1}, "1 batch uses this plan. Delete the batches first."},
		{plan.Detail{Members: 3, Batches: 2}, "3 members and 2 batches use this plan. Move the members to another plan and delete the batches first."},
		{plan.Detail{Members: 3, Default: true}, "This is the default plan. Clear the default plan in Settings first."},
	}
	for _, c := range cases {
		if got := blockedReason(c.d); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.d, got, c.want)
		}
	}
}

func TestPlanTimeText(t *testing.T) {
	for m, want := range map[int]string{0: "No limit", 1: "1 min", 90: "90 min", 60: "1 hour", 480: "8 hours"} {
		if got := timeText(m); got != want {
			t.Errorf("%d: got %q, want %q", m, got, want)
		}
	}
}

func TestPlanLimitText(t *testing.T) {
	if got := limitText(0, "GB"); got != "No limit" {
		t.Errorf("zero: %q", got)
	}
	if got := limitText(1.5, "GB"); got != "1.5 GB" {
		t.Errorf("value: %q", got)
	}
}
