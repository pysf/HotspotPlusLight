package update

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hotspotpluslight/internal/domain"
)

var testLog = slog.New(slog.DiscardHandler)

// pendingEnv is a data folder with a program file (new), exe.old, hsp.db (bad) and a database backup.
type pendingEnv struct {
	dir, exe string
	p        domain.PendingUpdate
	now      time.Time
}

func newPendingEnv(t *testing.T) *pendingEnv {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "prog")
	w := func(name, body string) {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(exe, "NEW")
	w(exe+".old", "OLD")
	w(filepath.Join(dir, "hsp.db"), "BADDB")
	w(filepath.Join(dir, "hsp.db-wal"), "wal")
	w(filepath.Join(dir, "hsp.db-shm"), "shm")
	backup := filepath.Join(dir, "hsp.db.bak-v1.0.0")
	w(backup, "GOODDB")
	now := time.Unix(10000, 0)
	e := &pendingEnv{dir: dir, exe: exe, now: now,
		p: domain.PendingUpdate{FromVersion: "v1.0.0", ToVersion: "v1.1.0", StartedAt: now.Unix(), Exe: exe, OldExe: exe + ".old", BackupDB: backup}}
	if err := WritePending(dir, e.p); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *pendingEnv) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (e *pendingEnv) has(name string) bool {
	_, err := os.Stat(filepath.Join(e.dir, name))
	return err == nil
}

func (e *pendingEnv) tries(t *testing.T) int {
	t.Helper()
	p, err := domain.ParsePendingUpdate([]byte(e.read(t, PendingFile)))
	if err != nil {
		t.Fatal(err)
	}
	return p.Tries
}

func TestRecoverNoFile(t *testing.T) {
	r, err := Recover(t.TempDir(), "v1.0.0", time.Now(), testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("%v %v", r, err)
	}
}

func TestRecoverCountsStartsThenRollsBack(t *testing.T) {
	e := newPendingEnv(t)
	for want := 1; want <= 2; want++ {
		r, err := Recover(e.dir, "v1.1.0", e.now.Add(time.Second), testLog)
		if err != nil || r.Outcome != OutcomeProbation {
			t.Fatalf("start %d: %v %v", want, r, err)
		}
		if got := e.tries(t); got != want {
			t.Fatalf("tries = %d, want %d", got, want)
		}
	}
	// The new program started twice and never became healthy: the third start rolls back.
	r, err := Recover(e.dir, "v1.1.0", e.now.Add(time.Second), testLog)
	if err != nil || r.Outcome != OutcomeRolledBack {
		t.Fatalf("third start: %v %v", r, err)
	}
	e.checkRolledBack(t, "the new version did not start")

	// A crash loop must end here: the next start finds no state file and runs normally.
	r, err = Recover(e.dir, "v1.0.0", e.now.Add(time.Minute), testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("after rollback: %v %v", r, err)
	}
	if !e.has(MarkerFile) {
		t.Error("the marker must stay until it is dismissed")
	}
}

func (e *pendingEnv) checkRolledBack(t *testing.T, reason string) {
	t.Helper()
	if got := e.read(t, "prog"); got != "OLD" {
		t.Errorf("exe = %q", got)
	}
	if got := e.read(t, "prog.bad"); got != "NEW" {
		t.Errorf(".bad = %q", got)
	}
	if e.has("prog.old") {
		t.Error(".old must be gone")
	}
	if got := e.read(t, "hsp.db"); got != "GOODDB" {
		t.Errorf("db = %q", got)
	}
	if got := e.read(t, "hsp.db.failed-v1.1.0"); got != "BADDB" {
		t.Errorf("failed db = %q", got)
	}
	for _, f := range []string{"hsp.db-wal", "hsp.db-shm", PendingFile, "hsp.db.restore"} {
		if e.has(f) {
			t.Errorf("%s must be gone", f)
		}
	}
	if !e.has("hsp.db.bak-v1.0.0") {
		t.Error("the backup must be kept")
	}
	m, ok := ReadRollbackMarker(e.dir)
	if !ok || m.From != "v1.0.0" || m.To != "v1.1.0" || m.Reason != reason || m.At < e.now.Unix() {
		t.Errorf("marker = %+v %v", m, ok)
	}
}

func TestRecoverOldFileAloneNeverRollsBack(t *testing.T) {
	e := newPendingEnv(t)
	r, err := Recover(e.dir, "v1.1.0", e.now.Add(24*time.Hour), testLog)
	if err != nil || r.Outcome != OutcomeProbation {
		t.Fatalf("%v %v", r, err)
	}
	if e.read(t, "prog") != "NEW" || e.read(t, "hsp.db") != "BADDB" {
		t.Error("nothing may change")
	}
}

func TestRecoverOldProgramDiscardsFile(t *testing.T) {
	e := newPendingEnv(t)
	r, err := Recover(e.dir, "v1.0.0", e.now, testLog)
	if err != nil || r.Outcome != OutcomeNone || e.has(PendingFile) {
		t.Fatalf("%v %v", r, err)
	}
	if e.read(t, "prog") != "NEW" {
		t.Error("nothing else may change")
	}
}

func TestRecoverBadFileIsRemoved(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, PendingFile), []byte("junk"), 0o600)
	r, err := Recover(dir, "v1.1.0", time.Now(), testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("%v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(dir, PendingFile)); err == nil {
		t.Error("file must be removed")
	}
}

// checkGaveUp checks that a failed rollback left the new version in place, with a note, and no state file.
func (e *pendingEnv) checkGaveUp(t *testing.T) {
	t.Helper()
	if e.has(PendingFile) || e.has("hsp.db.restore") {
		t.Error("state file and temp copy must be gone")
	}
	m, ok := ReadRollbackMarker(e.dir)
	if !ok || !m.NotRestored || m.Reason == "" || m.To != "v1.1.0" {
		t.Errorf("marker = %+v %v", m, ok)
	}
}

func TestRecoverMissingBackupStillStarts(t *testing.T) {
	e := newPendingEnv(t)
	os.Remove(e.p.BackupDB)
	e.p.Tries = 2
	WritePending(e.dir, e.p)
	r, err := Recover(e.dir, "v1.1.0", e.now, testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("%v %v", r, err)
	}
	e.checkGaveUp(t)
	if e.read(t, "prog") != "NEW" || e.read(t, "hsp.db") != "BADDB" || !e.has("prog.old") {
		t.Error("nothing may change")
	}
	// No loop: the next start finds no state file.
	if r, err := Recover(e.dir, "v1.1.0", e.now, testLog); err != nil || r.Outcome != OutcomeNone {
		t.Errorf("next start: %v %v", r, err)
	}
}

func TestRecoverFailedPathIsADirectoryStillStarts(t *testing.T) {
	e := newPendingEnv(t)
	failed := filepath.Join(e.dir, "hsp.db.failed-v1.1.0")
	os.MkdirAll(filepath.Join(failed, "sub"), 0o755)
	e.p.Tries = 2
	WritePending(e.dir, e.p)
	r, err := Recover(e.dir, "v1.1.0", e.now, testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("%v %v", r, err)
	}
	e.checkGaveUp(t)
	// The database step comes first and the program file last, so a failed database step changes neither.
	if e.read(t, "prog") != "NEW" || e.read(t, "hsp.db") != "BADDB" || !e.has("prog.old") || e.has("prog.bad") {
		t.Error("nothing may change")
	}
}

func TestRecoverGivesUpAfterTooManyAttempts(t *testing.T) {
	e := newPendingEnv(t)
	e.p.Tries = domain.PendingGiveUp
	WritePending(e.dir, e.p)
	r, err := Recover(e.dir, "v1.1.0", e.now, testLog)
	if err != nil || r.Outcome != OutcomeNone {
		t.Fatalf("%v %v", r, err)
	}
	e.checkGaveUp(t)
	if e.read(t, "prog") != "NEW" || e.read(t, "hsp.db") != "BADDB" {
		t.Error("no new attempt may be made")
	}
}

func TestRollbackDatabaseFirstThenProgramFile(t *testing.T) {
	e := newPendingEnv(t)
	// Break the last step: the old .bad file cannot be removed because it is a directory with content.
	os.MkdirAll(filepath.Join(e.exe+".bad", "sub"), 0o755)
	err := Rollback(e.dir, e.p, "x", e.now, testLog)
	if err == nil {
		t.Fatal("want an error")
	}
	// The database step was done and is remembered, the program file is still the new one.
	if e.read(t, "hsp.db") != "GOODDB" || e.read(t, "prog") != "NEW" {
		t.Errorf("db %q exe %q", e.read(t, "hsp.db"), e.read(t, "prog"))
	}
	p, perr := domain.ParsePendingUpdate([]byte(e.read(t, PendingFile)))
	if perr != nil || !p.DBRestored {
		t.Errorf("state file after a partial rollback: %+v %v", p, perr)
	}
}

func TestRollbackRerunAfterPartialFailureSkipsFinishedSteps(t *testing.T) {
	e := newPendingEnv(t)
	// State after a crash between the database step and the program file step.
	os.WriteFile(filepath.Join(e.dir, "hsp.db"), []byte("GOODDB"), 0o600)
	os.WriteFile(filepath.Join(e.dir, "hsp.db.failed-v1.1.0"), []byte("BADDB"), 0o600)
	os.Remove(filepath.Join(e.dir, "hsp.db-wal"))
	os.Remove(filepath.Join(e.dir, "hsp.db-shm"))
	os.WriteFile(e.p.BackupDB, []byte("CHANGED-BACKUP"), 0o600) // proves the database is not copied again
	e.p.DBRestored = true
	if err := Rollback(e.dir, e.p, "x", e.now, testLog); err != nil {
		t.Fatal(err)
	}
	if e.read(t, "hsp.db") != "GOODDB" || e.read(t, "hsp.db.failed-v1.1.0") != "BADDB" || e.read(t, "prog") != "OLD" || e.read(t, "prog.bad") != "NEW" {
		t.Error("rerun changed finished steps")
	}
	// Run it once more with everything done: the program file is the old one already.
	if err := Rollback(e.dir, e.p, "x", e.now, testLog); err != nil {
		t.Fatalf("second rerun: %v", err)
	}
	if e.read(t, "prog") != "OLD" || e.read(t, "prog.bad") != "NEW" {
		t.Error("second rerun changed the program file")
	}
}

func TestUndoTry(t *testing.T) {
	e := newPendingEnv(t)
	Recover(e.dir, "v1.1.0", e.now, testLog)
	if e.tries(t) != 1 {
		t.Fatal("tries must be 1")
	}
	if err := UndoTry(e.dir); err != nil || e.tries(t) != 0 {
		t.Fatalf("undo: %v tries %d", err, e.tries(t))
	}
	if err := UndoTry(e.dir); err != nil || e.tries(t) != 0 {
		t.Fatalf("undo at zero: %v tries %d", err, e.tries(t))
	}
	ClearPending(e.dir)
	if err := UndoTry(e.dir); err != nil || e.has(PendingFile) {
		t.Errorf("no file: %v", err)
	}
}

func TestClearPendingRetry(t *testing.T) {
	e := newPendingEnv(t)
	if err := ClearPendingRetry(e.dir, testLog, 3, time.Millisecond); err != nil || e.has(PendingFile) {
		t.Fatalf("clear: %v", err)
	}
	// A path that cannot be deleted: a directory with content.
	os.MkdirAll(filepath.Join(e.dir, PendingFile, "sub"), 0o755)
	start := time.Now()
	if err := ClearPendingRetry(e.dir, testLog, 3, 20*time.Millisecond); err == nil {
		t.Error("want an error")
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Error("must wait between the tries")
	}
}

func TestRollbackReplacesOldLeftovers(t *testing.T) {
	e := newPendingEnv(t)
	os.WriteFile(e.exe+".bad", []byte("EARLIER"), 0o644)
	os.WriteFile(filepath.Join(e.dir, "hsp.db.failed-v1.1.0"), []byte("EARLIER"), 0o644)
	if err := Rollback(e.dir, e.p, "x", e.now, testLog); err != nil {
		t.Fatal(err)
	}
	if e.read(t, "prog.bad") != "NEW" || e.read(t, "hsp.db.failed-v1.1.0") != "BADDB" {
		t.Error("old leftovers must be replaced")
	}
}

func TestWritePendingMode(t *testing.T) {
	e := newPendingEnv(t)
	st, err := os.Stat(filepath.Join(e.dir, PendingFile))
	if err != nil || st.Mode().Perm()&0o077 != 0 && !isWindows() {
		t.Errorf("mode %v %v", st, err)
	}
	if e.has(PendingFile + ".tmp") {
		t.Error("temp file left")
	}
}

func TestDismissRollbackMarker(t *testing.T) {
	e := newPendingEnv(t)
	Rollback(e.dir, e.p, "x", e.now, testLog)
	if _, ok := ReadRollbackMarker(e.dir); !ok {
		t.Fatal("no marker")
	}
	if err := DismissRollbackMarker(e.dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadRollbackMarker(e.dir); ok {
		t.Error("marker must be gone")
	}
	if err := DismissRollbackMarker(e.dir); err != nil {
		t.Errorf("second dismiss: %v", err)
	}
}

func TestChooseRestartMode(t *testing.T) {
	cases := []struct {
		win  bool
		inv  string
		want RestartMode
	}{{true, "", RestartByManager}, {false, "abc", RestartByManager}, {true, "abc", RestartByManager}, {false, "", RestartByChild}}
	for _, c := range cases {
		if got := ChooseRestartMode(c.win, c.inv); got != c.want {
			t.Errorf("%v %q = %v", c.win, c.inv, got)
		}
	}
}

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080/healthz",
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:8080":   "http://127.0.0.1:8080/healthz",
		"[::]:8080":      "http://127.0.0.1:8080/healthz",
		"[::1]:8080":     "http://[::1]:8080/healthz",
		"192.168.1.5:80": "http://192.168.1.5:80/healthz",
	}
	for in, want := range cases {
		if got := HealthURL(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}

func TestWaitHealthy(t *testing.T) {
	n := 0
	err := WaitHealthy(context.Background(), func(context.Context) error {
		n++
		if n < 3 {
			return errors.New("not yet")
		}
		return nil
	}, time.Second, time.Millisecond)
	if err != nil || n != 3 {
		t.Errorf("healthy: %v after %d", err, n)
	}
	start := time.Now()
	err = WaitHealthy(context.Background(), func(context.Context) error { return errors.New("down") }, 50*time.Millisecond, 5*time.Millisecond)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestHealthClientIgnoresProxyAndRedirects(t *testing.T) {
	tr, ok := healthClient.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatalf("the health client must have a transport without a proxy: %#v", healthClient.Transport)
	}
	// A proxy that would answer 503 for everything. The probe must not use it.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/healthz", http.StatusFound)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	if err := ProbeHealth(context.Background(), srv.URL+"/healthz"); err != nil {
		t.Errorf("probe through the environment proxy: %v", err)
	}
	if err := ProbeHealth(context.Background(), srv.URL+"/redirect"); err == nil {
		t.Error("a redirect is not a healthy answer")
	}
}
