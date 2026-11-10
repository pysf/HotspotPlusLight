package domain

import (
	"strings"
	"testing"
	"time"
)

func TestParsePendingUpdate(t *testing.T) {
	ok := PendingUpdate{FromVersion: "v1.0.0", ToVersion: "v1.1.0", StartedAt: 100, Exe: "/a", OldExe: "/a.old", BackupDB: "/db"}
	b, _ := ok.Marshal()
	got, err := ParsePendingUpdate(b)
	if err != nil || got != ok {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	bad := map[string]string{
		"not json":   `x`,
		"no version": `{"exe":"a","old_exe":"b","backup_db":"c","started_at":1}`,
		"no exe":     `{"to_version":"v1.0.0","old_exe":"b","backup_db":"c","started_at":1}`,
		"no old":     `{"to_version":"v1.0.0","exe":"a","backup_db":"c","started_at":1}`,
		"no backup":  `{"to_version":"v1.0.0","exe":"a","old_exe":"b","started_at":1}`,
		"no time":    `{"to_version":"v1.0.0","exe":"a","old_exe":"b","backup_db":"c"}`,
		"neg tries":  `{"to_version":"v1.0.0","exe":"a","old_exe":"b","backup_db":"c","started_at":1,"tries":-1}`,
		"empty":      ``,
		"empty json": `{}`,
	}
	for name, in := range bad {
		if _, err := ParsePendingUpdate([]byte(in)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseRollbackMarker(t *testing.T) {
	m := RollbackMarker{From: "v1.0.0", To: "v1.1.0", Reason: "x", At: 5}
	b, _ := m.Marshal()
	if got, err := ParseRollbackMarker(b); err != nil || got != m {
		t.Errorf("round trip: %v %+v", err, got)
	}
	for _, in := range []string{``, `{}`, `{"to":"v1.0.0"}`} {
		if _, err := ParseRollbackMarker([]byte(in)); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestDecidePending(t *testing.T) {
	start := time.Unix(1000, 0)
	p := PendingUpdate{ToVersion: "v1.1.0", StartedAt: start.Unix()}
	cases := []struct {
		name    string
		running string
		tries   int
		now     time.Time
		want    PendingAction
	}{
		{"new program, first start", "v1.1.0", 0, start.Add(time.Second), PendingProbation},
		{"no v prefix", "1.1.0", 1, start.Add(time.Second), PendingProbation},
		{"second start is still allowed", "v1.1.0", 1, start.Add(time.Minute), PendingProbation},
		{"two starts already", "v1.1.0", 2, start.Add(time.Second), PendingRollback},
		{"many starts", "v1.1.0", 7, start.Add(time.Second), PendingRollback},
		{"an old file alone never rolls back", "v1.1.0", 0, start.Add(24 * time.Hour), PendingProbation},
		{"old program", "v1.0.0", 0, start.Add(time.Second), PendingDiscard},
		{"old program, old file", "v1.0.0", 5, start.Add(time.Hour), PendingDiscard},
		{"dev build", "dev", 0, start, PendingDiscard},
	}
	for _, c := range cases {
		p.Tries = c.tries
		d := DecidePending(p, c.running, c.now)
		if d.Action != c.want {
			t.Errorf("%s: action %v, want %v", c.name, d.Action, c.want)
		}
		if (d.Action == PendingRollback) != (d.Reason != "") {
			t.Errorf("%s: reason %q", c.name, d.Reason)
		}
	}
}

func TestRollbackMarkerMessage(t *testing.T) {
	m := RollbackMarker{To: "v1.1.0", Reason: "r", At: 1}
	if got := m.Message(); !strings.Contains(got, "was rolled back (r)") || !strings.Contains(got, "lost") {
		t.Errorf("rolled back: %q", got)
	}
	m.NotRestored = true
	if got := m.Message(); !strings.Contains(got, "could not be put back (r)") || strings.Contains(got, "was rolled back") {
		t.Errorf("not restored: %q", got)
	}
}
