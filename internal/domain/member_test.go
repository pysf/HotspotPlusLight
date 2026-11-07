package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeMobile(t *testing.T) {
	for in, want := range map[string]string{"0912 345 6789": "9123456789", "+98 912-345-6789": "9123456789", "9123456789": "9123456789", "": ""} {
		if got, ok := NormalizeMobile(in); !ok || got != want {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"12345", "abc", "۰۹۱۲۳۴۵۶۷۸۹"} { // Persian digits are not converted (O13)
		if _, ok := NormalizeMobile(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	for _, in := range []string{"AA-BB-CC-DD-EE-FF", "aa:bb:cc:dd:ee:ff", "AABB.CCDD.EEFF", "aabbccddeeff", " aa bb cc dd ee ff "} {
		if got, ok := NormalizeMAC(in); !ok || got != "AA:BB:CC:DD:EE:FF" {
			t.Errorf("%q -> %q %v", in, got, ok)
		}
	}
	for _, in := range []string{"zz:zz:zz:zz:zz:zz", "aa:bb", "aabbccddeeff00", "AA:BB:CC:DD:EE:GG"} {
		if _, ok := NormalizeMAC(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseMember(t *testing.T) {
	loc := time.FixedZone("x", 3600)
	ok := MemberForm{Username: " Bob1 ", PlanID: "3", Active: true, ExpiresAt: "2026-12-31T23:59", Email: "a@b.co"}
	in, errs := ParseMember(ok, loc, true)
	if len(errs) != 0 || in.Username != "Bob1" || in.PlanID != 3 || in.ExpiresAt == nil || in.ExpiresAt.UTC().Hour() != 22 {
		t.Fatalf("%+v %v", in, errs)
	}
	// Username defaults to the mobile number on create only.
	in, errs = ParseMember(MemberForm{Mobile: "0912 345 6789", PlanID: "1"}, loc, true)
	if len(errs) != 0 || in.Username != "9123456789" {
		t.Fatalf("default username: %+v %v", in, errs)
	}
	if _, errs = ParseMember(MemberForm{Mobile: "0912 345 6789", PlanID: "1"}, loc, false); errs["username"] == "" {
		t.Fatal("update must not default the username")
	}
	bad := MemberForm{Username: strings.Repeat("a", 65), PlanID: "x", Mobile: "12", MAC: "no", Email: "nope", ExpiresAt: "tomorrow"}
	_, errs = ParseMember(bad, loc, true)
	for _, f := range []string{"username", "plan_id", "mobile", "mac", "email", "expires_at"} {
		if errs[f] == "" {
			t.Errorf("no error for %s", f)
		}
	}
}

func TestMemberStatusBoundary(t *testing.T) {
	now := time.Unix(1000, 0)
	soon := ExpiryLimit(now, MemberExpiringDays) - now.Unix()
	at := func(s int64) *time.Time { v := time.Unix(s, 0); return &v }
	for _, tc := range []struct {
		name   string
		active bool
		exp    *time.Time
		want   string
	}{
		{"no expiry", true, nil, "active"},
		{"disabled", false, nil, "inactive"},
		{"one second past", true, at(999), "expired"},
		{"exactly now", true, at(1000), "expiring"},
		{"disabled and past", false, at(999), "expired"},
		{"disabled and soon", false, at(1000 + 60), "inactive"},
		{"exactly 3 days left", true, at(1000 + soon), "expiring"},
		{"3 days and 1 s left", true, at(1000 + soon + 1), "active"},
		{"far away", true, at(1000 + 30*soon), "active"},
	} {
		if got := MemberStatus(tc.active, tc.exp, now); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	// Sub-second clock: compared in whole seconds, like the SQL filter.
	if got := MemberStatus(true, at(1000), time.Unix(1000, 900_000_000)); got != StatusExpiring {
		t.Errorf("same second: %s", got)
	}
}

func TestFormatRelative(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "in under 1 min"}, {-30 * time.Second, "under 1 min ago"},
		{5 * time.Minute, "in 5 min"}, {-5 * time.Minute, "5 min ago"},
		{3 * time.Hour, "in 3 h"}, {-47 * time.Hour, "47 h ago"},
		{48 * time.Hour, "in 2 days"}, {-72 * time.Hour, "3 days ago"}, {100 * 24 * time.Hour, "in 100 days"},
	} {
		if got := FormatRelative(now, now.Add(tc.d)); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestNormalizePaging(t *testing.T) {
	if p, per, pages := NormalizePaging(0, 7, 45); p != 1 || per != DefaultPageSize || pages != 3 {
		t.Fatalf("%d %d %d", p, per, pages)
	}
	if p, _, _ := NormalizePaging(99, 10, 45); p != 5 {
		t.Fatalf("clamp: %d", p)
	}
	if p, _, pages := NormalizePaging(3, 10, 0); p != 1 || pages != 1 {
		t.Fatalf("empty: %d %d", p, pages)
	}
}

func TestParseExtraBulk(t *testing.T) {
	if b, errs := ParseExtraBulk("1.5", "GB"); errs != nil || b != 1610612736 {
		t.Fatalf("%d %v", b, errs)
	}
	for _, tc := range [][2]string{{"-1", "GB"}, {"x", "GB"}, {"1", "TB"}, {"", "MB"}} {
		if _, errs := ParseExtraBulk(tc[0], tc[1]); len(errs) == 0 {
			t.Errorf("%v accepted", tc)
		}
	}
}

func TestBulkAmountsAreCappedInBytes(t *testing.T) {
	if _, errs := ParseExtraBulk("1000000000000", "GB"); errs["value"] == "" {
		t.Fatalf("1e12 GB must be rejected: %v", errs)
	}
	if b, errs := ParseExtraBulk("1000", "GB"); len(errs) != 0 || b <= 0 {
		t.Fatalf("%d %v", b, errs)
	}
	f := NewPlanForm()
	f.Name, f.BulkValue, f.BulkUnit = "P", "1000000000000", "GB"
	if _, errs := ParsePlan(f); errs["bulk_value"] == "" {
		t.Fatalf("plan bulk 1e12 GB must be rejected: %v", errs)
	}
}

func TestNormalizeMACIgnoresLineBreaks(t *testing.T) {
	got, ok := NormalizeMAC("aa:bb:cc:dd:ee:ff\r\n")
	if !ok || got != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
