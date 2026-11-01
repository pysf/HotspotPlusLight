package session

import (
	"context"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
)

type fakeRepo struct{ n int }

func (f fakeRepo) CountOpenSessions(context.Context) (int, error) { return f.n, nil }

// FakeDisconnector records targets; reusable by later phases' tests.
type FakeDisconnector struct {
	Calls []Target
	Err   error
}

func (f *FakeDisconnector) Disconnect(_ context.Context, t Target) error {
	f.Calls = append(f.Calls, t)
	return f.Err
}

func TestOpenCount(t *testing.T) {
	s := New(fakeRepo{2}, &FakeDisconnector{}, clock.NewFake(time.Unix(0, 0)))
	if n, err := s.OpenCount(context.Background()); err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
}
