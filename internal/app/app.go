// Package app is the composition root: the only place that wires concrete
// types together. It has no globals and no init().
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/config"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/linkstatus"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/plan"
	"hotspotpluslight/internal/radius"
	"hotspotpluslight/internal/routerpack"
	"hotspotpluslight/internal/secret"
	"hotspotpluslight/internal/session"
	"hotspotpluslight/internal/settings"
	"hotspotpluslight/internal/store"
	"hotspotpluslight/internal/usage"
	"hotspotpluslight/internal/voucher"
	"hotspotpluslight/internal/web/admin"
	"hotspotpluslight/internal/web/static"
)

// Options carries the process-level dependencies. A zero value selects the production default.
type Options struct {
	Getenv     func(string) string // default os.Getenv
	Stdout     io.Writer           // first-start block with the admin login; default os.Stdout
	Logger     *slog.Logger        // default text logger on LogWriter at HSP_LOG_LEVEL
	LogWriter  io.Writer           // default os.Stderr (the Windows service uses a log file)
	Clock      clock.Clock         // default real clock
	Migrations fs.FS               // default embedded migrations (tests inject others)
	// Disconnector sends RADIUS Disconnect-Requests (BR-18). Default: the real
	// RADIUS client (internal/radius); tests inject a recording fake.
	Disconnector session.Disconnector
	// AccountingBeforeCommit runs inside each accounting transaction just before the
	// commit; an error rolls it back and the packet gets no reply (crash injection, E4.15).
	AccountingBeforeCommit func() error
	// Ready, if set, is called by Run once all listeners are bound. The Windows service reports Running then.
	Ready func()
}

// Addrs are the bound listener addresses. They are useful when the configured port is 0.
type Addrs struct{ Admin, RadiusAuth, RadiusAcct string }

type services struct {
	auth      *auth.Service
	evaluator *auth.Evaluator
	member    *member.Service
	plan      *plan.Service
	settings  *settings.Service
	session   *session.Service
	usage     *usage.Service
	voucher   *voucher.Service
}

// App is the assembled application: database, services, admin HTTP server and RADIUS server.
type App struct {
	cfg    config.Config
	log    *slog.Logger
	store  *store.Store
	svc    services
	radius *radius.Server
	// enforcer is layer 2 of the quota enforcement (O3/O4): it disconnects online members who must not be online.
	enforcer *session.Enforcer

	adminSrv           *http.Server
	authConn, acctConn net.PacketConn
	adminLn            net.Listener
	addrs              Addrs
	addrMu             sync.RWMutex // guards addrs, which Start writes and admin handlers read
	stopOnce           sync.Once
	pruneCancel        context.CancelFunc
	pruneDone          chan struct{}
}

// New loads config, opens and migrates the database, verifies the key, seeds
// first-boot data and wires the services. It binds nothing and returns the first
// error; the database is closed in that case.
func New(ctx context.Context, opts Options) (*App, error) {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Clock == nil {
		opts.Clock = clock.Real{}
	}
	cfg, err := config.LoadWithKey(opts.Getenv)
	if err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		lw := opts.LogWriter
		if lw == nil {
			lw = os.Stderr
		}
		log = newLogger(lw, cfg)
	}
	box, err := secret.NewBox(cfg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("HSP_SECRET_KEY: %w", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "hsp.db"))
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, log: log, store: st}
	fail := func(err error) (*App, error) { st.Close(); return nil, err }

	migs := opts.Migrations
	if migs == nil {
		migs = store.EmbeddedMigrations()
	}
	n, err := st.Migrate(ctx, migs, opts.Clock.Now)
	if err != nil {
		return fail(err)
	}
	if n > 0 {
		log.Info("migrations applied", "count", n)
	}

	first, err := st.IsFirstBoot(ctx)
	if err != nil {
		return fail(err)
	}
	if first {
		if err := a.seed(ctx, box, opts); err != nil {
			return fail(err)
		}
	} else {
		kc, found, err := st.KeyCheck(ctx)
		if err != nil {
			return fail(err)
		}
		if !found {
			return fail(errors.New("settings.key_check is empty: database is not usable with this version"))
		}
		if err := box.VerifyKeyCheck(kc); err != nil {
			return fail(err)
		}
	}

	// The settings row is the source of truth for the server settings. Env vars only seeded it on first boot.
	dbSt, err := st.GetSettings(ctx)
	if err != nil {
		return fail(fmt.Errorf("read settings: %w", err))
	}
	if err := a.applyServerSettings(dbSt); err != nil {
		return fail(err)
	}
	if opts.Logger == nil {
		lw := opts.LogWriter
		if lw == nil {
			lw = os.Stderr
		}
		log = newLogger(lw, a.cfg)
		a.log = log
	}
	warnIgnoredEnv(log, opts.Getenv, dbSt)

	link := linkstatus.New(opts.Clock)
	disc := opts.Disconnector
	if disc == nil {
		disc = radius.NewDisconnector(st, link, log)
	}
	sessOpts := []session.Option{session.WithLogger(log)}
	if opts.AccountingBeforeCommit != nil {
		sessOpts = append(sessOpts, session.WithBeforeCommit(opts.AccountingBeforeCommit))
	}
	enforcer := session.NewEnforcer(st, opts.Clock, log)
	sessOpts = append(sessOpts, session.WithEnforcer(enforcer))
	sess := session.New(sessionRepo{st}, disc, opts.Clock, sessOpts...)
	a.enforcer = enforcer
	members := member.New(st, box, sess, opts.Clock) // kicks go through the session service, so an ACK ends the session row
	a.svc = services{
		auth:      auth.New(st, opts.Clock),
		evaluator: auth.NewEvaluator(st, box, opts.Clock, log),
		member:    members,
		plan:      plan.New(st, opts.Clock),
		settings:  settings.New(st, opts.Clock),
		session:   sess,
		usage:     usage.New(st, opts.Clock),
		voucher:   voucher.New(st, box, members, opts.Clock), // batch delete reuses the member delete path: kick, then remove
	}
	a.radius = radius.New(st, a.svc.evaluator, a.svc.session, link, log)
	pack, err := routerpack.New()
	if err != nil {
		return fail(err)
	}
	assets, err := static.New()
	if err != nil {
		return fail(err)
	}
	a.adminSrv = &http.Server{Handler: admin.Handler(admin.Deps{Auth: a.svc.auth, Plans: a.svc.plan, Members: a.svc.member, Batches: a.svc.voucher, Sessions: a.svc.session, Usage: a.svc.usage, Settings: a.svc.settings, Router: pack, Link: link, RadiusPorts: a.radiusPorts, RunningServer: a.runningServer, Assets: assets, Dev: cfg.Dev, Log: log}), ReadHeaderTimeout: 10 * time.Second}
	return a, nil
}

// newLogger returns a logger that writes key=value text to w, or JSON when HSP_LOG_FORMAT is json.
func newLogger(w io.Writer, cfg config.Config) *slog.Logger {
	ho := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(w, ho))
	}
	return slog.New(slog.NewTextHandler(w, ho))
}

// applyServerSettings copies the listen addresses and log settings from the settings row into the config.
func (a *App) applyServerSettings(st domain.Settings) error {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(st.LogLevel)); err != nil {
		return fmt.Errorf("settings log_level %q is invalid: %w", st.LogLevel, err)
	}
	if st.LogFormat != "text" && st.LogFormat != "json" {
		return fmt.Errorf("settings log_format %q is invalid (use text or json)", st.LogFormat)
	}
	a.cfg.AdminAddr, a.cfg.RadiusAuthAddr, a.cfg.RadiusAcctAddr = st.AdminAddr, st.RadiusAuthAddr, st.RadiusAcctAddr
	a.cfg.LogLevel, a.cfg.LogFormat = lvl, st.LogFormat
	return nil
}

// runningServer returns the server settings this process started with.
func (a *App) runningServer() domain.ServerSettings {
	return domain.ServerSettings{AdminAddr: a.cfg.AdminAddr, RadiusAuthAddr: a.cfg.RadiusAuthAddr, RadiusAcctAddr: a.cfg.RadiusAcctAddr,
		LogLevel: strings.ToLower(a.cfg.LogLevel.String()), LogFormat: a.cfg.LogFormat}
}

// warnIgnoredEnv logs a warning for each server env var that is set but differs from the saved setting.
// The saved setting wins after the first boot, so the env var has no effect.
func warnIgnoredEnv(log *slog.Logger, getenv func(string) string, st domain.Settings) {
	for _, e := range []struct{ env, saved string }{
		{"HSP_ADMIN_ADDR", st.AdminAddr}, {"HSP_RADIUS_AUTH_ADDR", st.RadiusAuthAddr}, {"HSP_RADIUS_ACCT_ADDR", st.RadiusAcctAddr},
		{"HSP_LOG_LEVEL", st.LogLevel}, {"HSP_LOG_FORMAT", st.LogFormat},
	} {
		if v := getenv(e.env); v != "" && !strings.EqualFold(v, e.saved) {
			log.Warn("env var ignored: the saved setting wins after the first start; change it on the Settings page",
				"env", e.env, "env_value", v, "saved_value", e.saved)
		}
	}
}

// sessionRepo adapts the store to session.Repo. The store's transaction type is
// concrete, and the service wants its own interface.
type sessionRepo struct{ *store.Store }

// InAcctTx runs fn in an accounting transaction of the store.
func (r sessionRepo) InAcctTx(ctx context.Context, fn func(session.AcctTx) error) error {
	return r.Store.InAcctTx(ctx, func(tx *store.AcctTx) error { return fn(tx) })
}

// seed creates the first-boot data (O15, BR-21): the admin account with the
// password "admin" and the must-change flag (BR-20), the RADIUS shared secret, and the server settings from the env.
// The stdout block says so.
func (a *App) seed(ctx context.Context, box *secret.Box, opts Options) error {
	hash, err := auth.HashPassword("admin")
	if err != nil {
		return err
	}
	kc, err := box.KeyCheck()
	if err != nil {
		return err
	}
	radiusSecret := a.cfg.RadiusSharedSecret
	if radiusSecret == "" {
		if radiusSecret, err = auth.GenerateSecret(24); err != nil {
			return err
		}
	}
	seeded, err := a.store.FirstBoot(ctx, store.SeedInput{
		AdminUsername:      a.cfg.AdminUsername,
		AdminPasswordHash:  hash,
		MustChangePassword: true,
		KeyCheck:           kc,
		RadiusSecret:       radiusSecret,
		AdminAddr:          a.cfg.AdminAddr,
		RadiusAuthAddr:     a.cfg.RadiusAuthAddr,
		RadiusAcctAddr:     a.cfg.RadiusAcctAddr,
		LogLevel:           strings.ToLower(a.cfg.LogLevel.String()),
		LogFormat:          a.cfg.LogFormat,
		Now:                opts.Clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("first-boot seeding: %w", err)
	}
	if !seeded {
		return nil
	}
	fmt.Fprintf(opts.Stdout, "=== HotspotPlus Lite first start ===\nLog in with username: %s  password: admin\nYou will be asked to set a new password right after you log in.\nForgot it later? Run: hotspotpluslight admin set-password\n====================================\n",
		a.cfg.AdminUsername)
	a.log.Info("admin account created with the default password; it must be changed at first login")
	return nil
}

// Start binds the admin HTTP socket and the two RADIUS sockets, and begins serving.
// It closes whatever it opened and returns an error if a bind fails.
func (a *App) Start() (err error) {
	var closers []io.Closer
	defer func() {
		if err != nil {
			for _, c := range closers {
				c.Close()
			}
		}
	}()
	if a.adminLn, err = net.Listen("tcp", a.cfg.AdminAddr); err != nil {
		return fmt.Errorf("listen admin_addr (saved in the settings table; see Troubleshooting to reset it): %w", err)
	}
	closers = append(closers, a.adminLn)
	if a.authConn, err = radius.Listen(a.cfg.RadiusAuthAddr, a.log); err != nil {
		return fmt.Errorf("listen radius_auth_addr (saved in the settings table; see Troubleshooting to reset it): %w", err)
	}
	closers = append(closers, a.authConn)
	if a.acctConn, err = radius.Listen(a.cfg.RadiusAcctAddr, a.log); err != nil {
		return fmt.Errorf("listen radius_acct_addr (saved in the settings table; see Troubleshooting to reset it): %w", err)
	}
	a.addrMu.Lock()
	a.addrs = Addrs{a.adminLn.Addr().String(), a.authConn.LocalAddr().String(), a.acctConn.LocalAddr().String()}
	a.addrMu.Unlock()
	go a.serveHTTP("admin", a.adminSrv, a.adminLn)
	a.startPruning()
	a.enforcer.Start(a.svc.session)
	a.radius.Serve(a.authConn, a.acctConn)
	a.log.Info("listening", "admin", a.addrs.Admin, "radius_auth", a.addrs.RadiusAuth, "radius_acct", a.addrs.RadiusAcct)
	return nil
}

// pruneEvery is how often old sessions are pruned after the run at start (O18).
const pruneEvery = 24 * time.Hour

// startPruning prunes old sessions once at start, so a restart applies a changed
// retention, and then daily until Shutdown. It runs in the background so RADIUS
// never waits for it. The timer is real, not the injected clock: it only
// schedules, and the cut-off itself comes from the Clock.
func (a *App) startPruning() {
	ctx, cancel := context.WithCancel(context.Background())
	a.pruneCancel, a.pruneDone = cancel, make(chan struct{})
	run := func() {
		if _, err := a.svc.session.Prune(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("session pruning failed", "err", err)
		}
	}
	go func() {
		defer close(a.pruneDone)
		run()
		t := time.NewTicker(pruneEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				run()
			}
		}
	}()
}

// serveHTTP serves s on ln until Shutdown and logs any other stop.
func (a *App) serveHTTP(name string, s *http.Server, ln net.Listener) {
	if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.log.Error("http listener stopped", "listener", name, "err", err)
	}
}

// Addrs returns the bound listener addresses. They are empty before Start.
func (a *App) Addrs() Addrs {
	a.addrMu.RLock()
	defer a.addrMu.RUnlock()
	return a.addrs
}

// radiusPorts returns the UDP ports to put into the router setup script. The bound
// port wins; before Start, or if it cannot be read, the configured one, then 1812 and 1813.
func (a *App) radiusPorts() (authPort, acctPort int) {
	port := func(addrs ...string) int {
		for _, addr := range addrs {
			if _, p, err := net.SplitHostPort(addr); err == nil {
				if n, _ := strconv.Atoi(p); n > 0 {
					return n
				}
			}
		}
		return 0
	}
	b := a.Addrs()
	authPort = cmp.Or(port(b.RadiusAuth, a.cfg.RadiusAuthAddr), 1812)
	acctPort = cmp.Or(port(b.RadiusAcct, a.cfg.RadiusAcctAddr), 1813)
	return authPort, acctPort
}

// Shutdown stops accepting, drains work in flight, then closes the database.
// It does this once; later calls return nil.
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	a.stopOnce.Do(func() {
		if a.pruneCancel != nil {
			a.pruneCancel()
			<-a.pruneDone
		}
		err = errors.Join(a.adminSrv.Shutdown(ctx), a.radius.Shutdown(ctx))
		a.enforcer.Stop() // after RADIUS, so no new checks are queued; it waits for the running ones
		err = errors.Join(err, a.store.Close())
		a.log.Info("shutdown complete")
	})
	return err
}

// SetAdminPassword is the `admin set-password` subcommand. It opens the existing
// database without migrating or seeding it, applies BR-20, stores the new hash,
// clears the must-change flag and deletes every admin session. It returns an error if
// there is no database or no admin yet. The password is never printed or logged.
func SetAdminPassword(ctx context.Context, opts Options, password string) error {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Clock == nil {
		opts.Clock = clock.Real{}
	}
	// The key is not needed here, so this never creates a key file or a data folder.
	dbPath := filepath.Join(config.DataDir(opts.Getenv), "hsp.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no database at %s: run `hotspotpluslight serve` once, or check HSP_DATA_DIR", dbPath)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	err = auth.New(st, opts.Clock).SetAdminPassword(ctx, password)
	if errors.Is(err, domain.ErrNotFound) {
		return errors.New("the database has no admin account yet: run `hotspotpluslight serve` once")
	}
	return err
}

// Run is the `serve` subcommand. It starts the app, waits until ctx ends, then
// shuts down gracefully (10 s at most).
func Run(ctx context.Context, opts Options) error {
	a, err := New(ctx, opts)
	if err != nil {
		return err
	}
	if err := a.Start(); err != nil {
		a.store.Close()
		return err
	}
	if opts.Ready != nil {
		opts.Ready()
	}
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.Shutdown(sctx)
}

// EnforcementIdle reports whether no automatic-disconnect check is queued or running. Tests wait for it.
func (a *App) EnforcementIdle() bool { return a.enforcer.Idle() }
