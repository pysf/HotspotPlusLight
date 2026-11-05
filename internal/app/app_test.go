package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/config"
	"hotspotpluslight/internal/secret"
	"hotspotpluslight/internal/store"
)

// When another process seeds the database between our first-boot check and our seed, this process
// must still check its key against the stored one (review B11).
func TestLostFirstBootRaceStillChecksTheKey(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hsp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Migrate(ctx, store.EmbeddedMigrations(), clock.Real{}.Now); err != nil {
		t.Fatal(err)
	}
	box := func(b byte) *secret.Box {
		k := make([]byte, 32)
		for i := range k {
			k[i] = b
		}
		bx, err := secret.NewBox(k)
		if err != nil {
			t.Fatal(err)
		}
		return bx
	}
	a := &App{cfg: config.Config{AdminUsername: "admin"}, log: slog.New(slog.DiscardHandler), store: st}
	opts := Options{Clock: clock.Real{}, Stdout: io.Discard}
	if err := a.initKey(ctx, box(1), true, opts); err != nil {
		t.Fatalf("first start: %v", err)
	}
	// Both processes saw an empty database; the second one lost the race and has another key.
	if err := a.initKey(ctx, box(2), true, opts); err == nil {
		t.Fatal("a process with the wrong key went on after losing the first-boot race")
	}
	if err := a.initKey(ctx, box(1), true, opts); err != nil {
		t.Fatalf("a process with the right key: %v", err)
	}
}
