// Package harness starts the real assembled app for black-box tests: real
// sockets on ephemeral ports, a temp DATA_DIR and an optional fake clock.
package harness

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"hotspotpluslight/internal/app"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/session"
	"hotspotpluslight/internal/update"
)

// FakeRestarter counts the restarts an update asks for.
type FakeRestarter struct{ n atomic.Int32 }

// Restart records the call.
func (f *FakeRestarter) Restart() error { f.n.Add(1); return nil }

// Calls returns how many restarts were asked for.
func (f *FakeRestarter) Calls() int { return int(f.n.Load()) }

// Options configures one start. The zero value means a fresh temp dir and a fresh key.
type Options struct {
	DataDir    string            // reuse to simulate a restart
	SecretKey  string            // base64; "" = generate a random one
	NoKey      bool              // do not set HSP_SECRET_KEY, so the app uses or creates {DataDir}/hsp.key
	Env        map[string]string // extra HSP_* variables (without prefix)
	Clock      clock.Clock
	Migrations fs.FS
	// Disconnector replaces the production Disconnect client (see FakeDisconnector).
	Disconnector session.Disconnector
	// AccountingBeforeCommit runs inside every accounting transaction just before
	// it commits; an error rolls it back and the packet gets no reply (E4.15).
	AccountingBeforeCommit func() error
	// Version is the build version for the update check ("" = dev build, which never checks).
	Version string
	// UpdateFirstDelay is the wait before the first background update check (0 = the 30 s default).
	UpdateFirstDelay time.Duration
	// ExePath is the program file an update replaces ("" = the test binary, which cannot update itself).
	ExePath string
	// Restarter is called after an update swapped the files (nil = a FakeRestarter that only counts).
	Restarter update.Restarter
	// Logger receives the server log (default: discarded).
	Logger *slog.Logger
}

// Server is a started app plus what tests need to inspect it.
type Server struct {
	App       *app.App
	Addrs     app.Addrs
	DataDir   string
	SecretKey string
	Stdout    *SafeBuffer
	Secret    string // current RADIUS shared secret as read from the DB at start
	cancel    context.CancelFunc
	done      chan error
	stopOnce  sync.Once
}

// SafeBuffer is a concurrency-safe bytes.Buffer.
type SafeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *SafeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *SafeBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// NewKey returns a valid random HSP_SECRET_KEY.
func NewKey() string {
	k := make([]byte, 32)
	rand.Read(k)
	return base64.StdEncoding.EncodeToString(k)
}

func (o Options) env(dir, key string) map[string]string {
	e := map[string]string{
		"HSP_DATA_DIR":         dir,
		"HSP_ADMIN_ADDR":       "127.0.0.1:0",
		"HSP_RADIUS_AUTH_ADDR": "127.0.0.1:0",
		"HSP_RADIUS_ACCT_ADDR": "127.0.0.1:0",
	}
	if !o.NoKey {
		e["HSP_SECRET_KEY"] = key
	}
	for k, v := range o.Env {
		e["HSP_"+k] = v
	}
	return e
}

// TryStart starts the app and returns the startup error instead of failing.
func TryStart(opts Options) (*Server, error) {
	dir := opts.DataDir
	if dir == "" {
		d, err := os.MkdirTemp("", "hsp-e2e-")
		if err != nil {
			return nil, err
		}
		dir = d
	}
	key := opts.SecretKey
	if key == "" {
		key = NewKey()
	}
	env := opts.env(dir, key)
	out := &SafeBuffer{}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	restarter := opts.Restarter
	if restarter == nil {
		restarter = &FakeRestarter{} // never the real one: it would end the test process
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, app.Options{
		Getenv:       func(k string) string { return env[k] },
		Stdout:       out,
		Logger:       logger,
		Clock:        opts.Clock,
		Migrations:   opts.Migrations,
		Disconnector: opts.Disconnector,

		Version:          opts.Version,
		ExePath:          opts.ExePath,
		Restarter:        restarter,
		UpdateFirstDelay: opts.UpdateFirstDelay,

		AccountingBeforeCommit: opts.AccountingBeforeCommit,
	})
	if err != nil {
		cancel()
		return &Server{DataDir: dir, SecretKey: key, Stdout: out}, err
	}
	if err := a.Start(); err != nil {
		cancel()
		return nil, err
	}
	s := &Server{App: a, Addrs: a.Addrs(), DataDir: dir, SecretKey: key, Stdout: out, cancel: cancel, done: make(chan error, 1)}
	return s, nil
}

// Start starts the app or fails the test; it is stopped on cleanup.
func Start(t *testing.T, opts Options) *Server {
	t.Helper()
	if opts.DataDir == "" {
		opts.DataDir = t.TempDir()
	}
	s, err := TryStart(opts)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s
}

// Stop shuts the app down gracefully (idempotent).
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		if s.App == nil {
			return
		}
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		s.App.Shutdown(ctx)
		s.cancel()
	})
}

// DBPath is the SQLite file location.
func (s *Server) DBPath() string { return filepath.Join(s.DataDir, "hsp.db") }

// DB opens the database directly (read/inspect only; tests may also tamper).
func DB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "hsp.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Get performs an HTTP GET with a short timeout.
func Get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// Binary builds the real CLI once per test run and returns its path.
func Binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hsp-bin-")
		if err != nil {
			buildErr = err
			return
		}
		name := "hotspotpluslight"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binPath = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/hotspotpluslight")
		cmd.Dir = moduleRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = &buildError{string(out), err}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return binPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return "go build: " + e.err.Error() + "\n" + e.out }

func moduleRoot() string {
	wd, _ := os.Getwd()
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			return wd
		}
	}
}

// ModuleRoot returns the repository root (for layering and README tests).
func ModuleRoot() string { return moduleRoot() }
