package domain

import (
	"fmt"
	"testing"
	"time"
)

func TestValidateAdminPassword(t *testing.T) { // BR-20
	cases := []struct {
		name, pw string
		ok       bool
	}{
		{"good", "Abcdefghijklmn1!xyz", true},
		{"exactly16", "Abcdefghijklm1!x", true},
		{"short15", "Abcdefghijkl1!x", false},
		{"no upper", "abcdefghijklmn1!xyz", false},
		{"no lower", "ABCDEFGHIJKLMN1!XYZ", false},
		{"no digit", "Abcdefghijklmnop!xyz", false},
		{"no symbol", "Abcdefghijklmnop1xyz", false},
		{"equals username", "admin", false},
	}
	for _, c := range cases {
		err := ValidateAdminPassword(c.pw, "admin")
		if (err == nil) != c.ok {
			t.Errorf("%s: got err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
	if ValidateAdminPassword("Abcdefghijklmn1!xyz", "Abcdefghijklmn1!xyz") == nil {
		t.Error("password equal to username must be rejected")
	}
}

func TestBatchExpiry(t *testing.T) {
	tehran, _ := time.LoadLocation("Asia/Tehran")
	now := time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC) // 00:30 on 2 Nov in Tehran
	if got, want := BatchExpiry(now, tehran, 30), time.Date(2026, 12, 3, 0, 0, 0, 0, tehran); !got.Equal(want) {
		t.Fatalf("%s, want %s", got, want)
	}
	if got, want := BatchExpiry(now, time.UTC, 30), time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("%s, want %s", got, want)
	}
}

func TestVoucherPages(t *testing.T) {
	for n, want := range map[int][]int{0: nil, 1: {1}, 14: {14}, 15: {14, 1}, 29: {14, 14, 1}, 30: {14, 14, 2}} {
		var got []int
		for _, p := range VoucherPages(make([]Voucher, n)) {
			got = append(got, len(p))
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%d vouchers: pages %v, want %v", n, got, want)
		}
	}
}

// BatchExpiry must be a real instant at local 00:00 (or the first instant after
// it when midnight is skipped by a DST jump), never off by an hour from 24h arithmetic.
func TestBatchExpiryAcrossDST(t *testing.T) {
	cases := []struct {
		zone string
		now  time.Time // UTC
		days int
	}{
		{"Europe/Berlin", time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC), 10},  // spans 29 Mar spring-forward
		{"Europe/Berlin", time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC), 0},   // lands on 29 Mar 00:00 (CET)
		{"Europe/Berlin", time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), 10}, // spans 25 Oct fall-back
		{"Europe/Berlin", time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), 0},  // 25 Oct 00:00 (CEST)
		{"America/New_York", time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC), 5},
		{"America/Sao_Paulo", time.Date(2018, 11, 2, 12, 0, 0, 0, time.UTC), 1}, // 4 Nov 2018: 00:00 does not exist
		{"America/Sao_Paulo", time.Date(2018, 11, 3, 12, 0, 0, 0, time.UTC), 0}, // same skipped midnight
		{"Asia/Tehran", time.Date(2022, 3, 20, 12, 0, 0, 0, time.UTC), 3},       // last Iranian DST era
		{"Asia/Tehran", time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC), 30},
		{"Asia/Beirut", time.Date(2023, 3, 24, 12, 0, 0, 0, time.UTC), 1}, // 26 Mar 2023: 00:00 skipped
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Skipf("no tzdata for %s: %v", c.zone, err)
		}
		got := BatchExpiry(c.now, loc, c.days).In(loc)
		y, m, d := c.now.In(loc).Date()
		wantDay := time.Date(y, m, d+c.days+1, 12, 0, 0, 0, loc) // noon always exists: gives the target date
		if got.Year() != wantDay.Year() || got.Month() != wantDay.Month() || got.Day() != wantDay.Day() {
			t.Errorf("%s %s+%d: %s is not on %s", c.zone, c.now, c.days, got, wantDay.Format("2006-01-02"))
			continue
		}
		if got.Hour() != 0 || got.Minute() != 0 {
			// Midnight skipped: it must be the first valid instant of that day, i.e. the
			// instant one second earlier belongs to the previous local day.
			if prev := got.Add(-time.Second); prev.Day() == got.Day() {
				t.Errorf("%s: %s is not the start of the local day", c.zone, got)
			}
		}
		if got.Before(c.now) {
			t.Errorf("%s: expiry %s before now", c.zone, got)
		}
	}
}
