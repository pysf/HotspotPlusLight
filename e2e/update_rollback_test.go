package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/update"
)

// buildVersion builds the real program with the given version into dir and returns its path.
func buildVersion(t *testing.T, dir, version string) string {
	t.Helper()
	out := filepath.Join(dir, "build-"+version)
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", out, "./cmd/hotspotpluslight")
	cmd.Dir = harness.ModuleRoot()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	return out
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// E22.21 the real program: a new version that cannot start (its database is broken) is rolled back on the
// third start. The old program file and the database copy come back, the failed files are kept, and the old
// program then runs normally. INVOCATION_ID makes the program act as under systemd: it exits instead of starting a child.
func TestE22_21_RealBinaryRollsBackACrashingVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the rollback of the real program is tested on Unix; Windows CI runs the file-level tests")
	}
	bins := t.TempDir()
	oldBin := buildVersion(t, bins, "v1.0.0")
	newBin := buildVersion(t, bins, "v1.1.0")

	// A good database made by the old version, kept as the backup.
	s := harness.Start(t, harness.Options{Version: "v1.0.0"})
	dataDir, secretKey := s.DataDir, s.SecretKey
	s.Stop()
	backup := filepath.Join(dataDir, "hsp.db.bak-v1.0.0")
	good := readFile(t, filepath.Join(dataDir, "hsp.db"))
	if err := os.WriteFile(backup, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	// The new version "migrated" the database into something it cannot open.
	if err := os.WriteFile(filepath.Join(dataDir, "hsp.db"), []byte("this is not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"hsp.db-wal", "hsp.db-shm"} {
		os.Remove(filepath.Join(dataDir, f))
	}

	progDir := t.TempDir()
	exe := filepath.Join(progDir, "prog")
	copyExe := func(src, dst string) {
		if err := os.WriteFile(dst, []byte(readFile(t, src)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyExe(newBin, exe)
	copyExe(oldBin, exe+".old")
	p := domain.PendingUpdate{FromVersion: "v1.0.0", ToVersion: "v1.1.0", StartedAt: time.Now().Unix(), Exe: exe, OldExe: exe + ".old", BackupDB: backup}
	if err := update.WritePending(dataDir, p); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(), "HSP_DATA_DIR="+dataDir, "INVOCATION_ID=e2e", "HSP_SECRET_KEY="+secretKey)
	exitCode := func() (int, string) {
		cmd := exec.Command(exe, "serve")
		cmd.Env = env
		var out lockedBuf
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), out.String()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0, out.String()
	}
	pendingTries := func() int {
		p, err := domain.ParsePendingUpdate([]byte(readFile(t, filepath.Join(dataDir, "update-pending.json"))))
		if err != nil {
			t.Fatal(err)
		}
		return p.Tries
	}

	for start := 1; start <= 2; start++ {
		if code, out := exitCode(); code != 1 {
			t.Fatalf("start %d: exit %d, want 1\n%s", start, code, out)
		}
		if got := pendingTries(); got != start {
			t.Fatalf("start %d: tries = %d", start, got)
		}
	}
	code, out := exitCode()
	if code != update.RestartExitCode {
		t.Fatalf("third start: exit %d, want %d\n%s", code, update.RestartExitCode, out)
	}
	if v, err := exec.Command(exe, "version").Output(); err != nil || strings.TrimSpace(string(v)) != "v1.0.0" {
		t.Errorf("program after the rollback: %q %v", v, err)
	}
	if !exists(exe+".bad") || exists(exe+".old") {
		t.Error(".bad must exist and .old must be gone")
	}
	if got := readFile(t, filepath.Join(dataDir, "hsp.db")); got != good {
		t.Error("the database was not restored")
	}
	if got := readFile(t, filepath.Join(dataDir, "hsp.db.failed-v1.1.0")); got != "this is not a database" {
		t.Errorf("failed db = %q", got)
	}
	if exists(filepath.Join(dataDir, "update-pending.json")) || !exists(filepath.Join(dataDir, "update-rolled-back.json")) {
		t.Error("state file must be gone and the marker must exist")
	}

	// The restarted old program runs normally and shows the note.
	cmd := exec.Command(exe, "serve")
	cmd.Env = env
	var logs lockedBuf
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	defer func() {
		if t.Failed() {
			t.Log(logs.String())
		}
	}()
	waitFor(t, "old program listening", func() bool { return strings.Contains(logs.String(), "listening") })
	if m, ok := update.ReadRollbackMarker(dataDir); !ok || m.To != "v1.1.0" {
		t.Errorf("marker = %+v %v", m, ok)
	}
}
