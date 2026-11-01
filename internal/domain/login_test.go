package domain

import (
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
	plan := Plan{SpeedValue: 1, SpeedUnit: "Mbps", BulkValue: 10, BulkUnit: "GB", TimeLimitMinutes: 60}
	g := GrantFor(QuotaOf(MemberState{UsedSeconds: 600}, plan), plan, 60)
	if g.SessionTimeout != 3000 || !g.XmitClamped || g.XmitLimit != MaxAttr32 || g.InterimSeconds != 60 {
		t.Errorf("%+v", g)
	}
	free := Plan{}
	g = GrantFor(QuotaOf(MemberState{}, free), free, 30)
	if g.SessionTimeout != UnlimitedSessionTimeout || g.XmitLimit != UnlimitedXmitLimit || g.XmitClamped || g.RateLimit != "" {
		t.Errorf("%+v", g)
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

func TestGrantForClampsNegativeValues(t *testing.T) {
	g := GrantFor(Quota{RemainingSecs: -5, RemainingBytes: -9}, Plan{}, -1)
	if g.SessionTimeout != 0 || g.XmitLimit != 0 || g.InterimSeconds != 0 {
		t.Errorf("%+v", g)
	}
}
