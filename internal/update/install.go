package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"hotspotpluslight/internal/domain"
)

const (
	maxArchive     = 200 << 20 // the biggest download we accept
	maxSums        = 1 << 20   // the biggest SHA256SUMS we accept
	installTimeout = 10 * time.Minute
	sumsName       = "SHA256SUMS"
)

// State is the step the installer is in.
type State string

// The steps of an install, in order. Idle is also the state after a restart of the program.
const (
	StateIdle        State = ""
	StateDownloading State = "downloading"
	StateVerifying   State = "verifying"
	StateBackingUp   State = "backing up"
	StateReplacing   State = "replacing"
	StateRestarting  State = "restarting"
	StateDone        State = "done"
	StateFailed      State = "failed"
)

// Progress is what the page shows about the install.
type Progress struct {
	State   State
	Version string // the version being installed
	Reason  string // friendly text, set when State is StateFailed
}

// Running is true while the install is working or waiting for the restart. The page polls then.
func (p Progress) Running() bool {
	return p.State != StateIdle && p.State != StateDone && p.State != StateFailed
}

// ErrBusy means an install is already running (or finished and waits for the restart).
var ErrBusy = errors.New("an update is already running")

// RefusedError means the update cannot start. Reason is a friendly text.
type RefusedError struct{ Reason string }

func (e *RefusedError) Error() string { return e.Reason }

// Restarter restarts the program after the files were swapped.
// A real restarter may never return, because it ends the process.
type Restarter interface{ Restart() error }

// Backuper copies the database to a file.
type Backuper interface {
	BackupTo(ctx context.Context, path string) error
}

// PlanResult says if an update can be installed here. Reason is set when it cannot.
type PlanResult struct {
	OK     bool
	Reason string
}

// Plan decides if this program can update itself: not a dev or test build, a file for this OS and CPU in the release,
// and a program folder we may write to. assetsKnown is false before the first check since the start.
func Plan(exe string, dev bool, assets []string, assetsKnown bool, goos, goarch string) PlanResult {
	base := filepath.Base(exe)
	reason := domain.InstallBlocker(domain.InstallFacts{
		Dev:         dev,
		TestBinary:  strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe"),
		AssetsKnown: assetsKnown,
		Assets:      assets,
		GOOS:        goos,
		GOARCH:      goarch,
		Writable:    exe != "" && dirWritable(filepath.Dir(exe)),
	})
	return PlanResult{OK: reason == "", Reason: reason}
}

// dirWritable tries to create a file in dir. The swap needs this, and permission bits alone do not tell.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".hsp-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	return os.Remove(name) == nil
}

// Installer downloads a release, checks it, backs up the database and swaps the program file.
// One install runs at a time, as one background job.
type Installer struct {
	ck        *Checker
	backup    Backuper
	restarter Restarter
	log       *slog.Logger
	exe       string
	dataDir   string
	goos      string
	goarch    string
	client    *http.Client
	runCheck  func(exe string) error // checks that a program file runs; tests replace it

	mu      sync.Mutex
	prog    Progress
	swapped bool // the new program file is in place; no second install until the restart
}

// NewInstaller returns an Installer for the program file exe. Backups and downloads go to dataDir.
func NewInstaller(ck *Checker, backup Backuper, restarter Restarter, exe, dataDir string) *Installer {
	i := &Installer{ck: ck, backup: backup, restarter: restarter, log: ck.log, exe: exe, dataDir: dataDir, goos: runtime.GOOS, goarch: runtime.GOARCH, runCheck: CheckRunnable}
	i.client = &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if !domain.DownloadLinkOK(req.URL.String(), ck.allowHTTP()) {
			return errors.New("redirect to a link that is not https")
		}
		return nil
	}}
	return i
}

// SetRunCheck replaces the check that the new program file runs. Tests use it, because their fake program files do not run.
func (i *Installer) SetRunCheck(f func(exe string) error) { i.runCheck = f }

// Progress returns the state of the install.
func (i *Installer) Progress() Progress {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.prog
}

func (i *Installer) set(p Progress) {
	i.mu.Lock()
	i.prog = p
	i.mu.Unlock()
}

func (i *Installer) step(s State, version string) {
	i.log.Info("update step", "step", string(s), "version", version)
	i.set(Progress{State: s, Version: version})
}

// Plan says if an update can be installed now.
func (i *Installer) Plan() PlanResult {
	assets, known := i.ck.Assets()
	return Plan(i.exe, i.ck.isDev(), assets, known, i.goos, i.goarch)
}

// Start begins an install as a background job and returns at once. It returns ErrBusy when one is running,
// and a *RefusedError when the setting is off or this program cannot update itself.
func (i *Installer) Start(ctx context.Context) error {
	st, err := i.ck.Status(ctx)
	if err != nil {
		return err
	}
	switch {
	case st.Dev:
		return &RefusedError{"This is a development build. It cannot update itself."}
	case !st.Enabled:
		return &RefusedError{"Update checks are off. Turn them on in Settings to update."}
	}
	if p := i.Plan(); !p.OK {
		return &RefusedError{p.Reason}
	}
	i.mu.Lock()
	if i.prog.Running() || i.prog.State == StateDone || i.swapped {
		i.mu.Unlock()
		return ErrBusy
	}
	if _, err := os.Stat(filepath.Join(i.dataDir, PendingFile)); err == nil {
		i.mu.Unlock()
		return &RefusedError{"The last update is not finished yet. Wait until the program is healthy, then try again."}
	}
	i.prog = Progress{State: StateDownloading, Version: st.Latest}
	i.mu.Unlock()
	go i.run()
	return nil
}

// userError is a failure with a friendly text for the page. The cause goes to the log.
type userError struct {
	msg   string
	cause error
}

func (e *userError) Error() string {
	if e.cause == nil {
		return e.msg
	}
	return e.msg + ": " + e.cause.Error()
}

func (e *userError) Unwrap() error { return e.cause }

func fail(msg string, cause error) error { return &userError{msg, cause} }

// run is the install job. A panic is logged and shown as a failure instead of ending the program.
func (i *Installer) run() {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			i.log.Error("update crashed", "panic", r)
			i.set(Progress{State: StateFailed, Reason: "Something went wrong. See the server log."})
		}
	}()
	version := ""
	err := i.install(ctx, &version)
	if err == nil {
		return
	}
	reason := "Something went wrong. See the server log."
	var ue *userError
	if errors.As(err, &ue) {
		reason = ue.msg
	}
	i.log.Warn("update failed", "err", err)
	i.set(Progress{State: StateFailed, Version: version, Reason: reason})
}

// install runs all steps. version is set as soon as the release is known.
func (i *Installer) install(ctx context.Context, version *string) error {
	i.log.Info("update started", "from", i.ck.version)
	rel, err := i.ck.fetch(ctx) // the stored result is not trusted
	if err != nil {
		return fail(friendly(err), err)
	}
	*version = rel.Tag
	if !domain.IsNewerVersion(rel.Tag, i.ck.version) {
		return fail("You already have the latest version.", nil)
	}
	i.step(StateDownloading, rel.Tag)
	name := domain.AssetName(i.goos, i.goarch)
	archiveLink, sumsLink := rel.AssetLinks[name], rel.AssetLinks[sumsName]
	if archiveLink == "" || sumsLink == "" {
		return fail("This release has no file for "+i.goos+"/"+i.goarch+" with a checksum.", nil)
	}
	for _, l := range []string{archiveLink, sumsLink} {
		if !domain.DownloadLinkOK(l, i.ck.allowHTTP()) {
			return fail("The download link of the release is not https.", errors.New(l))
		}
	}
	want, err := i.fetchSum(ctx, sumsLink, name)
	if err != nil {
		return err
	}
	archive, got, err := i.download(ctx, archiveLink)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	i.step(StateVerifying, rel.Tag)
	if got != want {
		return fail("The download does not match its checksum. Nothing was changed.", fmt.Errorf("want %s, got %s", want, got))
	}
	newExe := i.exe + ".new"
	if err := unpackBinary(archive, newExe, name); err != nil {
		os.Remove(newExe)
		return fail("The download could not be unpacked. Nothing was changed.", err)
	}
	if err := i.runCheck(newExe); err != nil {
		os.Remove(newExe)
		return fail("The new program does not run on this computer. Nothing was changed.", err)
	}

	i.step(StateBackingUp, rel.Tag)
	old := i.ck.version
	if v, ok := domain.ParseVersion(old); ok {
		old = v.String()
	}
	if err := i.backup.BackupTo(ctx, filepath.Join(i.dataDir, "hsp.db.bak-"+old)); err != nil {
		os.Remove(newExe)
		return fail("The database could not be backed up. Nothing was changed.", err)
	}

	i.step(StateReplacing, rel.Tag)
	// The state file comes before the swap: if the new program never gets healthy, the next start finds it and rolls back.
	pending := domain.PendingUpdate{FromVersion: old, ToVersion: rel.Tag, StartedAt: time.Now().Unix(),
		Exe: i.exe, OldExe: i.exe + ".old", BackupDB: filepath.Join(i.dataDir, "hsp.db.bak-"+old)}
	if err := WritePending(i.dataDir, pending); err != nil {
		os.Remove(newExe)
		return fail("The update could not be prepared. Nothing was changed.", err)
	}
	if err := swap(i.exe); err != nil {
		ClearPending(i.dataDir)
		// If the old file could not be put back either, exe.new may be the only copy left. Keep it.
		if _, statErr := os.Stat(i.exe); statErr == nil {
			os.Remove(newExe)
		}
		return fail("The program file could not be replaced. Nothing was changed.", err)
	}

	i.mu.Lock()
	i.swapped = true
	i.mu.Unlock()
	i.step(StateRestarting, rel.Tag)
	if err := i.restarter.Restart(); err != nil {
		ClearPending(i.dataDir)
		if uerr := unswap(i.exe); uerr != nil {
			return fail("The new version is installed, but the program could not restart itself. Restart it by hand.", errors.Join(err, uerr))
		}
		// The old program file is back, and this program still runs it: a new try is possible.
		i.mu.Lock()
		i.swapped = false
		i.mu.Unlock()
		return fail("The program could not restart itself with the new version. The old version was put back.", err)
	}
	i.set(Progress{State: StateDone, Version: rel.Tag})
	return nil
}

// get opens link. The caller closes the body.
func (i *Installer) get(ctx context.Context, link string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fail("The download link is not valid.", err)
	}
	req.Header.Set("User-Agent", "HotspotPlusLight/"+i.ck.version)
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fail("The download failed. Check the internet connection of this computer.", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fail(fmt.Sprintf("The download failed (GitHub answered %d).", resp.StatusCode), nil)
	}
	return resp, nil
}

// fetchSum downloads SHA256SUMS and returns the hash listed for name.
func (i *Installer) fetchSum(ctx context.Context, link, name string) (string, error) {
	resp, err := i.get(ctx, link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSums+1))
	if err != nil || len(body) > maxSums {
		return "", fail("The checksum file could not be read.", err)
	}
	sum, ok := domain.ParseSHA256Sums(string(body))[name]
	if !ok {
		return "", fail("The checksum file has no line for "+name+". Nothing was changed.", nil)
	}
	return sum, nil
}

// download saves link to a temp file in the data folder and returns its path and sha256 (lower-case hex).
func (i *Installer) download(ctx context.Context, link string) (path, sum string, err error) {
	resp, err := i.get(ctx, link)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	f, err := os.CreateTemp(i.dataDir, "update-*.tmp")
	if err != nil {
		return "", "", fail("The download could not be saved in the data folder.", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchive+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxArchive {
		err = errors.New("the download is too big")
	}
	if err != nil {
		os.Remove(f.Name())
		return "", "", fail("The download failed.", err)
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// swap renames the running program to exe.old and exe.new to exe. If the second rename fails it puts the old file back.
// Windows allows renaming a running program, but not overwriting it, so exe.old is removed first.
func swap(exe string) error {
	oldExe, newExe := exe+".old", exe+".new"
	if err := os.Remove(oldExe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, oldExe); err != nil {
		return err
	}
	if err := os.Rename(newExe, exe); err != nil {
		if rerr := os.Rename(oldExe, exe); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return nil
}

// unswap undoes swap after the restart failed: the new program file goes to exe.new and exe.old is renamed back.
// Windows allows renaming a running program, but not overwriting it, so exe.new is removed first. If the last rename fails the new file is put back.
func unswap(exe string) error {
	oldExe, newExe := exe+".old", exe+".new"
	if err := os.Remove(newExe); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(exe, newExe); err != nil {
		return err
	}
	if err := os.Rename(oldExe, exe); err != nil {
		if rerr := os.Rename(newExe, exe); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	os.Remove(newExe)
	return nil
}
