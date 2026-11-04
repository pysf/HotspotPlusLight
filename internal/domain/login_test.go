package domain

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestNormalizeUsername(t *testing.T) {
	for in, want := range map[string]bool{"alice": true, " Alice9 ": true, "": false, "a b": false, "a-b": false, "é": false, "۱۲۳": false, strings.Repeat("a", 64): true, strings.Repeat("a", 65): false} {
		if _, ok := NormalizeUsername(in); ok != want {
			t.Errorf("%q: %v", in, ok)
		}
	}
	if u, _ := NormalizeUsername(" Alice "); u != "Alice" {
		t.Errorf("not trimmed: %q", u)
	}
}

func TestRateLimit(t *testing.T) {
	for _, c := range []struct {
		v    float64
		unit string
		want string
	}{
		{0, "Mbps", ""},
		{512, "Kbps", "512k/512k 2048k/2048k 1024k/1024k 8/8"},
		{1.5, "Mbps", "1500k/1500k 6000k/6000k 3000k/3000k 8/8"},
		{3, "Gbps", "3000000k/3000000k 12000000k/12000000k 6000000k/6000000k 8/8"},
		{3.5, "Gbps", "3500000k"},
		{0.0004, "Kbps", "1k/1k 4k/4k 2k/2k 8/8"}, // never "0k"
	} {
		if got := RateLimit(Plan{SpeedValue: c.v, SpeedUnit: c.unit}); got != c.want {
			t.Errorf("%v %s: %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestGrantFor(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	plan := Plan{SpeedValue: 1, SpeedUnit: "Mbps", BulkValue: 10, BulkUnit: "GB", TimeLimitMinutes: 60, DurationDays: 30}
	g := GrantFor(MemberState{SubscriptionDate: now, UsedSeconds: 600}, plan, 60, now)
	if g.SessionTimeout != 3000 || !g.DataLimited || g.DataGiga != 2 || g.DataLow != uint32(10<<30-2<<32) || g.InterimSeconds != 60 {
		t.Errorf("%+v", g)
	}
	free := Plan{DurationDays: 1}
	g = GrantFor(MemberState{SubscriptionDate: now}, free, 30, now)
	if g.DataLimited || g.RateLimit != "" || g.SessionTimeout != 86400 {
		t.Errorf("unlimited plan: %+v", g)
	}
}

func TestGrantForTimeout(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	soon := now.Add(100 * time.Second)
	late := now.Add(5000 * time.Second)
	plan := Plan{DurationDays: 1, TimeLimitMinutes: 60}
	for name, c := range map[string]struct {
		m    MemberState
		p    Plan
		want uint32
	}{
		"time quota wins":    {MemberState{SubscriptionDate: now, UsedSeconds: 3500}, plan, 100},
		"expires_at wins":    {MemberState{SubscriptionDate: now, ExpiresAt: &soon}, plan, 100},
		"window end wins":    {MemberState{SubscriptionDate: now.Add(-86400 * time.Second).Add(100 * time.Second)}, plan, 100},
		"expires_at later":   {MemberState{SubscriptionDate: now, ExpiresAt: &late}, plan, 3600},
		"at least one":       {MemberState{SubscriptionDate: now, UsedSeconds: 3600}, plan, 1},
		"window already out": {MemberState{SubscriptionDate: now.Add(-2 * 86400 * time.Second)}, plan, 1},
		"capped at 32 bits":  {MemberState{SubscriptionDate: now}, Plan{DurationDays: 90000}, math.MaxUint32},
	} {
		if g := GrantFor(c.m, c.p, 60, now); g.SessionTimeout != c.want {
			t.Errorf("%s: %d, want %d", name, g.SessionTimeout, c.want)
		}
	}
}

func TestSplitBytes(t *testing.T) {
	for _, c := range []struct {
		n         int64
		low, giga uint32
	}{{-5, 0, 0}, {0, 0, 0}, {1<<32 - 1, 1<<32 - 1, 0}, {1 << 32, 0, 1}, {5<<32 + 7, 7, 5}} {
		if l, g := SplitBytes(c.n); l != c.low || g != c.giga {
			t.Errorf("%d: %d/%d", c.n, l, g)
		}
	}
}

func TestActiveSince(t *testing.T) {
	now := time.Unix(1000, 0)
	if got := ActiveSince(now, 60); got.Unix() != 880 {
		t.Errorf("%v", got)
	}
}

func TestReplyMessage(t *testing.T) {
	if got := ReasonOutOfTime.ReplyMessage(); got != "OUT_OF_TIME: Your time allowance is used up." {
		t.Error(got)
	}
	for r := range reasonMessages {
		if r.Message() == "" {
			t.Errorf("%s has no message", r)
		}
	}
}
