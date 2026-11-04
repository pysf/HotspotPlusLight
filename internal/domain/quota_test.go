package domain

import (
	"math"
	"testing"
	"time"
)

var now0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func plan10gb() Plan {
	return Plan{DurationDays: 30, BulkValue: 10, BulkUnit: "GB", TimeLimitMinutes: 60, MaxSessions: 1}
}

func state() MemberState { return MemberState{Active: true, SubscriptionDate: now0} }

func TestEvaluate(t *testing.T) {
	const gb = int64(1 << 30)
	at := func(s time.Time) *time.Time { return &s }
	cases := []struct {
		name   string
		mod    func(*MemberState, *Plan)
		want   Reason
		renew  bool
		atTime time.Time
	}{
		{"ok", func(*MemberState, *Plan) {}, "", false, now0},
		{"inactive", func(m *MemberState, _ *Plan) { m.Active = false }, ReasonInactive, false, now0},
		{"expired strictly after", func(m *MemberState, _ *Plan) { m.ExpiresAt = at(now0.Add(-time.Second)) }, ReasonExpired, false, now0},
		{"expires_at boundary is still valid", func(m *MemberState, _ *Plan) { m.ExpiresAt = at(now0) }, "", false, now0},
		{"window end is inclusive", func(*MemberState, *Plan) {}, "", false, now0.Add(30 * 24 * time.Hour)},
		{"window lapsed", func(*MemberState, *Plan) {}, ReasonNoSubscription, false, now0.Add(30*24*time.Hour + time.Second)},
		{"lapsed renews", func(m *MemberState, p *Plan) { p.AutoResubscribe = true; m.UsedDownload = 10 * gb }, "", true, now0.Add(31 * 24 * time.Hour)},
		{"out of data", func(m *MemberState, _ *Plan) { m.UsedDownload, m.UsedUpload = 6*gb, 4*gb }, ReasonOutOfBulk, false, now0},
		{"extra bulk helps", func(m *MemberState, _ *Plan) { m.UsedDownload = 10 * gb; m.ExtraBulkBytes = 1 }, "", false, now0},
		{"unlimited data", func(m *MemberState, p *Plan) { p.BulkValue = 0; m.UsedDownload = 99 * gb }, "", false, now0},
		{"out of time", func(m *MemberState, _ *Plan) { m.UsedSeconds = 3600 }, ReasonOutOfTime, false, now0},
		{"unlimited time", func(m *MemberState, p *Plan) { p.TimeLimitMinutes = 0; m.UsedSeconds = 1e9 }, "", false, now0},
		{"inactive wins over expired", func(m *MemberState, _ *Plan) { m.Active = false; m.ExpiresAt = at(now0.Add(-time.Hour)) }, ReasonInactive, false, now0},
	}
	for _, c := range cases {
		m, p := state(), plan10gb()
		c.mod(&m, &p)
		if d := Evaluate(m, p, c.atTime); d.Reason != c.want || d.Renew != c.renew {
			t.Errorf("%s: %+v, want reason %q renew %v", c.name, d, c.want, c.renew)
		}
	}
}

func TestQuotaText(t *testing.T) {
	m, p := state(), plan10gb()
	m.ExtraBulkBytes, m.UsedDownload, m.UsedUpload, m.UsedSeconds = 1<<30, 2<<30, 1<<29, 1800
	q := QuotaOf(m, p)
	if got := q.DataText("GB"); got != "2.5 GB / 11 GB (8.5 GB left)" {
		t.Error(got)
	}
	if got := q.TimeText(); got != "30 min / 60 min (30 min left)" {
		t.Error(got)
	}
	p.BulkValue, p.TimeLimitMinutes = 0, 0
	q = QuotaOf(m, p)
	if q.DataText("MB") != "2560 MB / unlimited" || q.TimeText() != "30 min / unlimited" {
		t.Errorf("%q %q", q.DataText("MB"), q.TimeText())
	}
	if !q.WindowEnd.Equal(now0.Add(30 * 24 * time.Hour)) {
		t.Error("window end")
	}
}

func TestQuotaOfSaturatesInsteadOfOverflowing(t *testing.T) {
	huge := Plan{DurationDays: 1 << 40, BulkValue: 1e12, BulkUnit: "GB", MaxSessions: 1}
	q := QuotaOf(MemberState{Active: true, SubscriptionDate: now0, ExtraBulkBytes: 1 << 62}, huge)
	if q.AllowedBytes <= 0 || q.RemainingBytes <= 0 {
		t.Fatalf("overflowed: %+v", q)
	}
	if !q.WindowEnd.After(now0) {
		t.Fatalf("window end overflowed: %v", q.WindowEnd)
	}
}

func TestShouldEnd(t *testing.T) {
	const gb = int64(1 << 30)
	past := now0.Add(-time.Second)
	later := now0.Add(31 * 24 * time.Hour)
	for name, c := range map[string]struct {
		mod  func(*MemberState, *Plan)
		at   time.Time
		want Reason
	}{
		"fine":                  {func(*MemberState, *Plan) {}, now0, ""},
		"out of data":           {func(m *MemberState, _ *Plan) { m.UsedUpload = 10 * gb }, now0, ReasonOutOfBulk},
		"out of time":           {func(m *MemberState, _ *Plan) { m.UsedSeconds = 3600 }, now0, ReasonOutOfTime},
		"expired":               {func(m *MemberState, _ *Plan) { m.ExpiresAt = &past }, now0, ReasonExpired},
		"inactive":              {func(m *MemberState, _ *Plan) { m.Active = false }, now0, ReasonInactive},
		"window ended":          {func(*MemberState, *Plan) {}, later, ReasonNoSubscription},
		"auto-renew window end": {func(_ *MemberState, p *Plan) { p.AutoResubscribe = true }, later, ReasonNoSubscription},
	} {
		m, p := state(), plan10gb()
		c.mod(&m, &p)
		got, end := ShouldEnd(m, p, c.at)
		if got != c.want || end != (c.want != "") {
			t.Errorf("%s: %q %v, want %q", name, got, end, c.want)
		}
	}
}

func TestQuotaOfSaturatesHugeUsage(t *testing.T) {
	m := MemberState{UsedDownload: math.MaxInt64, UsedUpload: math.MaxInt64}
	q := QuotaOf(m, Plan{BulkValue: 1, BulkUnit: "MB", DurationDays: 30})
	if q.UsedBytes < 0 || q.RemainingBytes != 0 {
		t.Fatalf("used %d remaining %d", q.UsedBytes, q.RemainingBytes)
	}
}
