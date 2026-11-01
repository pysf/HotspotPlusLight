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

// A session the admin kicked (ended, status not Stop) is completed by the router's Stop: same row, counted once, cause kept.
func TestApplyAccountingStopCompletesKickedSession(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	end := now.Add(-time.Minute)
	kicked := &Session{ID: 7, Username: "a", AcctSessionID: "s", NASIP: "1.1.1.1", Status: AcctInterim, StartedAt: now.Add(-time.Hour),
		EndedAt: &end, TerminateCause: "Admin-Reset", Cum: Counters{Download: 100, Upload: 10, Seconds: 50}}
	res := ApplyAccounting(kicked, AcctRecord{Status: AcctStop, Username: "a", SessionID: "s", NASIP: "1.1.1.1", TerminateCause: "User-Request",
		Cum: Counters{Download: 160, Upload: 14, Seconds: 70}}, now)
	if res.Ignore || res.Insert || res.Session.ID != 7 {
		t.Fatalf("must update the same row: %+v", res)
	}
	if res.Delta != (Counters{Download: 60, Upload: 4, Seconds: 20}) {
		t.Fatalf("delta %+v", res.Delta)
	}
	if res.Session.EndedAt == nil || !res.Session.EndedAt.Equal(end) || res.Session.TerminateCause != "Admin-Reset" || res.Session.Status != AcctStop {
		t.Fatalf("session %+v", res.Session)
	}
	// A retransmitted Stop is now a duplicate of a finished row.
	if again := ApplyAccounting(&res.Session, AcctRecord{Status: AcctStop, Username: "a", SessionID: "s", NASIP: "1.1.1.1", Cum: Counters{Download: 160, Upload: 14, Seconds: 70}}, now); !again.Ignore {
		t.Fatalf("retransmitted Stop not ignored: %+v", again)
	}
	// A Start with the same id after a kick is a new session.
	if st := ApplyAccounting(kicked, AcctRecord{Status: AcctStart, Username: "a", SessionID: "s", NASIP: "1.1.1.1"}, now); !st.Insert {
		t.Fatalf("Start must create a new row: %+v", st)
	}
}

// Only a Stop whose figures are all >= the stored ones completes a kick-ended row;
// an Interim, or a Stop with a counter reset (a reused Acct-Session-Id), must not
// overwrite the kicked row.
func TestApplyAccountingKickedRowIsStrict(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	end := now.Add(-time.Minute)
	kicked := &Session{ID: 7, Username: "a", AcctSessionID: "s", NASIP: "1.1.1.1", Status: AcctInterim, StartedAt: now.Add(-time.Hour),
		EndedAt: &end, TerminateCause: "Admin-Reset", Cum: Counters{Download: 100, Upload: 10, Seconds: 50}}
	mk := func(st AcctStatus, c Counters) AcctRecord {
		return AcctRecord{Status: st, Username: "a", SessionID: "s", NASIP: "1.1.1.1", Cum: c}
	}
	// A later Interim above the stored figures is not merged: new row.
	if r := ApplyAccounting(kicked, mk(AcctInterim, Counters{200, 20, 90}), now); !r.Insert || r.Session.ID != 0 {
		t.Fatalf("interim merged into the kicked row: %+v", r)
	}
	// A Stop with a counter reset (reused id) is not merged either: new row.
	if r := ApplyAccounting(kicked, mk(AcctStop, Counters{30, 5, 20}), now); !r.Ignore && (!r.Insert || r.Session.ID != 0) {
		t.Fatalf("reset Stop merged into the kicked row: %+v", r)
	}
	// One counter lower is a reset too.
	if r := ApplyAccounting(kicked, mk(AcctStop, Counters{300, 5, 90}), now); r.Session.ID == 7 {
		t.Fatalf("partly lower Stop merged: %+v", r)
	}
	// A late Interim that the kicked row already covers is ignored (duplicate guard).
	if r := ApplyAccounting(kicked, mk(AcctInterim, Counters{90, 10, 40}), now); !r.Ignore {
		t.Fatalf("covered Interim not ignored: %+v", r)
	}
	// A Stop equal to the stored figures completes the row with no delta.
	if r := ApplyAccounting(kicked, mk(AcctStop, kicked.Cum), now); r.Insert || r.Session.ID != 7 || r.Session.Status != AcctStop || !r.Delta.IsZero() {
		t.Fatalf("equal Stop: %+v", r)
	}
}
