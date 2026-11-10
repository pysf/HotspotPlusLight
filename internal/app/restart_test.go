package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/update"
)

// runEnv returns options for Run in a fresh data folder, with the state file for version v1.1.0 written.
func runEnv(t *testing.T) (Options, string, domain.PendingUpdate) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "prog")
	os.WriteFile(exe, []byte("NEW"), 0o755)
	os.WriteFile(exe+".old", []byte("OLD"), 0o755)
	os.WriteFile(filepath.Join(dir, "hsp.db.bak-v1.0.0"), []byte("x"), 0o600)
	p := domain.PendingUpdate{FromVersion: "v1.0.0", ToVersion: "v1.1.0", StartedAt: time.Now().Unix(),
		Exe: exe, OldExe: exe + ".old", BackupDB: filepath.Join(dir, "hsp.db.bak-v1.0.0")}
	if err := update.WritePending(dir, p); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HSP_DATA_DIR": dir, "HSP_ADMIN_ADDR": "127.0.0.1:0", "HSP_RADIUS_AUTH_ADDR": "127.0.0.1:0",
		"HSP_RADIUS_ACCT_ADDR": "127.0.0.1:0", "INVOCATION_ID": "test", // service manager mode: no child process
		"HSP_SECRET_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="}
	return Options{Getenv: func(k string) string { return env[k] }, Stdout: os.Stderr, Logger: slog.New(slog.DiscardHandler),
		Version: "v1.1.0", ExePath: exe}, dir, p
}

func TestHealthyNewVersionDeletesStateFile(t *testing.T) {
	opts, dir, _ := runEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()
	gone := func() bool { _, err := os.Stat(filepath.Join(dir, update.PendingFile)); return err != nil }
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && !gone(); time.Sleep(50 * time.Millisecond) {
	}
	if !gone() {
		t.Fatal("the state file must be deleted when the program is healthy")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUnhealthyNewVersionRollsBackAndAsksForRestart(t *testing.T) {
	opts, dir, p := runEnv(t)
	opts.HealthTimeout = 300 * time.Millisecond
	opts.HealthProbe = func(context.Context, string) error { return errors.New("down") }
	if err := Run(context.Background(), opts); !errors.Is(err, update.ErrRestart) {
		t.Fatalf("Run returned %v, want ErrRestart", err)
	}
	if b, _ := os.ReadFile(p.Exe); string(b) != "OLD" {
		t.Errorf("exe = %q", b)
	}
	if b, _ := os.ReadFile(p.Exe + ".bad"); string(b) != "NEW" {
		t.Errorf(".bad = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hsp.db")); string(b) != "x" {
		t.Errorf("db = %q", b)
	}
	if _, ok := update.ReadRollbackMarker(dir); !ok {
		t.Error("marker missing")
	}
	if _, err := os.Stat(filepath.Join(dir, update.PendingFile)); err == nil {
		t.Error("state file must be gone")
	}
}

func TestStartAfterTwoTriesRollsBackBeforeOpeningTheDatabase(t *testing.T) {
	opts, dir, p := runEnv(t)
	p.Tries = 2
	update.WritePending(dir, p)
	if err := Run(context.Background(), opts); !errors.Is(err, update.ErrRestart) {
		t.Fatalf("Run returned %v", err)
	}
	if b, _ := os.ReadFile(p.Exe); string(b) != "OLD" {
		t.Errorf("exe = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hsp.db")); string(b) != "x" {
		t.Errorf("db = %q", b)
	}
}

func TestInstallRestartEndsRunWithErrRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	opts, dir, _ := runEnv(t)
	update.ClearPending(dir)
	opts.Version = "v1.0.0"
	script := filepath.Join(dir, "newprog")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	opts.ExePath = script
	a, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.serve(context.Background(), func() {}) }()
	if err := (processRestarter{a: a}).Restart(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, update.ErrRestart) {
			t.Errorf("serve returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not end")
	}
}

func TestRestartRefusedWhenNewFileDoesNotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	opts, dir, _ := runEnv(t)
	update.ClearPending(dir)
	script := filepath.Join(dir, "broken")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0o755)
	opts.ExePath = script
	a, err := New(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.store.Close()
	if err := (processRestarter{a: a}).Restart(); err == nil {
		t.Fatal("want an error")
	}
	select {
	case <-a.stopReq:
		t.Error("the run must not stop")
	default:
	}
}

func TestRestartProcessByHandStartsChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	script := filepath.Join(dir, "prog")
	os.WriteFile(script, []byte("#!/bin/sh\necho started > "+marker+"\n"), 0o755)
	if err := restartProcess(update.RestartByChild, script, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
	}
	t.Fatal("the child did not run")
}

func TestRestartProcessErrors(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if err := restartProcess(update.RestartByChild, filepath.Join(t.TempDir(), "missing"), log); err == nil {
		t.Error("by hand with a missing file must fail")
	}
	if err := restartProcess(update.RestartByManager, "", log); !errors.Is(err, update.ErrRestart) {
		t.Errorf("manager mode: %v", err)
	}
}

func TestCleanStopDuringHealthWindowIsNotAFailedStart(t *testing.T) {
	opts, dir, _ := runEnv(t)
	probing := make(chan struct{}, 1) // the first probe means the app is up and the watcher runs
	opts.HealthProbe = func(context.Context, string) error {
		select {
		case probing <- struct{}{}:
		default:
		}
		return errors.New("not yet")
	}
	waitProbing := func() {
		t.Helper()
		select {
		case <-probing:
		case <-time.After(30 * time.Second):
			t.Fatal("the app did not start probing")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()
	waitProbing()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, update.PendingFile))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := domain.ParsePendingUpdate(b); p.Tries != 0 {
		t.Errorf("tries = %d after a deliberate stop, want 0", p.Tries)
	}
	// Many deliberate restarts never roll back.
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, opts) }()
		waitProbing()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("restart %d: %v", i, err)
		}
	}
	if _, ok := update.ReadRollbackMarker(dir); ok {
		t.Error("a deliberate restart must never roll back")
	}
}

func TestFailedRollbackAfterHealthTimeoutStillRestarts(t *testing.T) {
	opts, dir, p := runEnv(t)
	os.Remove(p.BackupDB) // the rollback cannot be done
	opts.HealthTimeout = 300 * time.Millisecond
	opts.HealthProbe = func(context.Context, string) error { return errors.New("down") }
	if err := Run(context.Background(), opts); !errors.Is(err, update.ErrRestart) {
		t.Fatalf("Run returned %v, want ErrRestart", err)
	}
	m, ok := update.ReadRollbackMarker(dir)
	if !ok || !m.NotRestored {
		t.Errorf("marker = %+v %v", m, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, update.PendingFile)); err == nil {
		t.Error("state file must be gone, or the restart would loop")
	}
	if b, _ := os.ReadFile(p.Exe); string(b) != "NEW" {
		t.Errorf("exe = %q", b)
	}
}
