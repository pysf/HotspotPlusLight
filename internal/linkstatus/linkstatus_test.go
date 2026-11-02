package linkstatus

import (
	"fmt"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
)

func TestRecorder(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	fc := clock.NewFake(t0)
	r := New(fc)
	if s := r.Snapshot(); !s.AccessRequest.At.IsZero() || s.BadSecretTotal != 0 {
		t.Fatalf("not empty: %+v", s)
	}
	r.AccessRequest("10.0.0.1:5000")
	fc.Advance(time.Second)
	r.Accounting("10.0.0.1:5001")
	r.Disconnect("10.0.0.1:3799", "ACK")
	s := r.Snapshot()
	if s.AccessRequest.From != "10.0.0.1" || !s.AccessRequest.At.Equal(t0) || s.Accounting.At.Equal(t0) || s.Disconnect.Detail != "ACK" {
		t.Errorf("%+v", s)
	}
	r.BadSecret("10.0.0.2:1")
	r.BadSecret("10.0.0.3:1")
	r.BadSecret("10.0.0.2:2")
	s = r.Snapshot()
	if s.BadSecretTotal != 3 || len(s.BadSecretSources) != 2 || s.BadSecretSources[0].From != "10.0.0.2" || s.BadSecretSources[0].Count != 2 {
		t.Errorf("%+v", s.BadSecretSources)
	}
}

func TestBadSourcesBounded(t *testing.T) {
	r := New(clock.NewFake(time.Unix(0, 0)))
	for i := range 100 {
		r.BadSecret(fmt.Sprintf("10.0.0.%d:1", i))
	}
	s := r.Snapshot()
	if s.BadSecretTotal != 100 || len(s.BadSecretSources) != maxBadSources || s.BadSecretSources[0].From != "10.0.0.99" {
		t.Errorf("total %d, sources %d", s.BadSecretTotal, len(s.BadSecretSources))
	}
}
