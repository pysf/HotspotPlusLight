package domain

import (
	"testing"
	"time"
)

var at = time.Unix(1_700_000_000, 0).UTC()

func TestCumulativeFromAcctGigawords(t *testing.T) {
	c := CumulativeFromAcct(10, 1, 20, 2, 99)
	if c.Upload != 1<<32+10 || c.Download != 2<<32+20 || c.Seconds != 99 {
		t.Fatalf("%+v", c)
	}
	if c := CumulativeFromAcct(0xFFFFFFFF, 0xFFFFFFFF, 0, 0, 0); c.Upload <= 0 {
		t.Fatal("overflow")
	}
}

func TestDeltaTo(t *testing.T) {
	prev := Counters{Download: 100, Upload: 50, Seconds: 10}
	cases := []struct{ cur, want Counters }{
		{Counters{150, 60, 20}, Counters{50, 10, 10}},
		{prev, Counters{}},                         // retransmission
		{Counters{30, 70, 5}, Counters{30, 20, 5}}, // per-field restart
		{Counters{0, 0, 0}, Counters{}},            // restart to zero
	}
	for _, c := range cases {
		if got := prev.DeltaTo(c.cur); got != c.want {
			t.Errorf("%+v -> %+v, want %+v", c.cur, got, c.want)
		}
	}
}

func rec(st AcctStatus, dl, ul, secs int64) AcctRecord {
	return AcctRecord{Status: st, Username: "u", SessionID: "s", NASIP: "1.1.1.1", Cum: Counters{dl, ul, secs}}
}

func TestApplyAccountingLifecycle(t *testing.T) {
	r := ApplyAccounting(nil, rec(AcctStart, 0, 0, 0), at)
	if !r.Insert || r.Session.Status != AcctStart || !r.Session.StartedAt.Equal(at) {
		t.Fatalf("start: %+v", r)
	}
	open := r.Session
	open.ID = 1
	// Start retransmission reuses the row.
	if r := ApplyAccounting(&open, rec(AcctStart, 0, 0, 0), at.Add(time.Second)); r.Insert {
		t.Fatal("start retransmission inserted")
	}
	r = ApplyAccounting(&open, rec(AcctInterim, 100, 10, 60), at.Add(time.Minute))
	if r.Insert || r.Delta != (Counters{100, 10, 60}) || r.Session.Status != AcctInterim {
		t.Fatalf("interim: %+v", r)
	}
	open = r.Session
	r = ApplyAccounting(&open, rec(AcctStop, 150, 10, 90), at.Add(2*time.Minute))
	if r.Delta != (Counters{50, 0, 30}) || r.Session.EndedAt == nil || r.Session.Status != AcctStop {
		t.Fatalf("stop: %+v", r)
	}
	done := r.Session
	// Retransmitted Stop and delayed Interim are ignored.
	if r := ApplyAccounting(&done, rec(AcctStop, 150, 10, 90), at.Add(3*time.Minute)); !r.Ignore {
		t.Fatal("stop retransmission not ignored")
	}
	if r := ApplyAccounting(&done, rec(AcctInterim, 100, 10, 60), at.Add(3*time.Minute)); !r.Ignore {
		t.Fatal("late interim not ignored")
	}
	// A Start with the same id after the Stop is a new session.
	if r := ApplyAccounting(&done, rec(AcctStart, 0, 0, 0), at.Add(time.Hour)); !r.Insert || r.Session.EndedAt != nil {
		t.Fatalf("reused id: %+v", r)
	}
}

func TestApplyAccountingInterimWithoutRow(t *testing.T) {
	r := ApplyAccounting(nil, rec(AcctInterim, 500, 5, 120), at)
	if !r.Insert || r.Delta != (Counters{500, 5, 120}) || !r.Session.StartedAt.Equal(at.Add(-2*time.Minute)) {
		t.Fatalf("%+v", r)
	}
}

func TestApplyAccountingStartClosesOpenRowWithTime(t *testing.T) {
	old := Session{ID: 7, Status: AcctInterim, Cum: Counters{100, 10, 60}, StartedAt: at}
	r := ApplyAccounting(&old, rec(AcctStart, 0, 0, 0), at.Add(time.Hour))
	if !r.Insert || r.Session.ID != 0 || !r.Delta.IsZero() || r.Session.EndedAt != nil {
		t.Fatalf("new row: %+v", r)
	}
	c := r.Closed
	if c == nil || c.ID != 7 || c.EndedAt == nil || !c.EndedAt.Equal(at.Add(time.Hour)) || c.TerminateCause != "Lost-Service" || c.Cum != old.Cum {
		t.Fatalf("closed: %+v", c)
	}
	// retransmitted Start (no time counted yet) closes nothing
	fresh := Session{ID: 8, Status: AcctStart}
	if r := ApplyAccounting(&fresh, rec(AcctStart, 0, 0, 0), at); r.Closed != nil || r.Insert {
		t.Fatalf("retransmission: %+v", r)
	}
}
