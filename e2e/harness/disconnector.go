package harness

import (
	"context"
	"errors"
	"sync"

	"hotspotpluslight/internal/domain"
)

// FakeDisconnector stands in for the RADIUS Disconnect client: it records every
// target and can be told to fail.
type FakeDisconnector struct {
	mu    sync.Mutex
	calls []domain.SessionTarget
	Fail  bool
}

func (f *FakeDisconnector) Disconnect(_ context.Context, t domain.SessionTarget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, t)
	if f.Fail {
		return errors.New("fake disconnect failure")
	}
	return nil
}

// Calls returns the targets disconnected so far.
func (f *FakeDisconnector) Calls() []domain.SessionTarget {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.SessionTarget(nil), f.calls...)
}

// Reset forgets the recorded calls.
func (f *FakeDisconnector) Reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}
