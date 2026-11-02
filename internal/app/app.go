// Package app is the composition root: the ONLY place that wires concrete
// types together. No globals, no init().
package app

import (
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
	"sync"
	"time"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/config"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/plan"
	"hotspotpluslight/internal/radius"
	"hotspotpluslight/internal/routerpack"
	"hotspotpluslight/internal/secret"
	"hotspotpluslight/internal/session"
	"hotspotpluslight/internal/settings"
	"hotspotpluslight/internal/store"
	"hotspotpluslight/internal/voucher"
	"hotspotpluslight/internal/web/admin"
	"hotspotpluslight/internal/web/pageapi"
)

// Options carries the process-level dependencies; zero values select production defaults.
type Options struct {
	Getenv     func(string) string // default os.Getenv
	Stdout     io.Writer           // first-start password block; default os.Stdout
	Logger     *slog.Logger        // default text logger on stderr at HSP_LOG_LEVEL
	Clock      clock.Clock         // default real clock
	Migrations fs.FS               // default embedded migrations (tests inject others)
	// Disconnector sends RADIUS Disconnect-Requests (BR-18). Default: the real
	// RADIUS client (internal/radius); tests inject a recording fake.
	Disconnector session.Disconnector
	// AccountingBeforeCommit runs inside each accounting transaction just before the
	// commit; an error rolls it back and the packet gets no reply (crash injection, E4.15).
	AccountingBeforeCommit func() error
}

// Addrs are the bound listener addresses (useful when configured with port 0).
type Addrs struct{ HTTP, Admin, RadiusAuth, RadiusAcct string }

type services struct {
	auth      *auth.Service
	evaluator *auth.Evaluator
	member    *member.Service
	plan      *plan.Service
	settings  *settings.Service
	session   *session.Service
	voucher   *voucher.Service
}

type App struct {
	cfg    config.Config
	log    *slog.Logger
	store  *store.Store
	svc    services
	radius *radius.Server

	httpSrv, adminSrv  *http.Server
	authConn, acctConn net.PacketConn
	httpLn, adminLn    net.Listener
	addrs              Addrs
	stopOnce           sync.Once
	pruneCancel        context.CancelFunc
	pruneDone          chan struct{}
}

// New loads config, opens and migrates the database, verifies the key, seeds
// first-boot data and wires the services. It binds nothing.
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
	cfg, err := config.Load(opts.Getenv)
	if err != nil {
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
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

	disc := opts.Disconnector
	if disc == nil {
		disc = radius.NewDisconnector(st, log)
	}
	sessOpts := []session.Option{session.WithLogger(log)}
	if opts.AccountingBeforeCommit != nil {
		sessOpts = append(sessOpts, session.WithBeforeCommit(opts.AccountingBeforeCommit))
	}
	sess := session.New(sessionRepo{st}, disc, opts.Clock, sessOpts...)
	members := member.New(st, box, sess, opts.Clock) // kicks go through the session service: ACK => session ended
	a.svc = services{
		auth:      auth.New(st, opts.Clock),
		evaluator: auth.NewEvaluator(st, box, opts.Clock, log),
		member:    members,
		plan:      plan.New(st, opts.Clock),
		settings:  settings.New(st, opts.Clock),
		session:   sess,
		voucher:   voucher.New(st, box, members, opts.Clock), // batch delete reuses the member delete path (kick, then remove)
	}
	a.radius = radius.New(st, a.svc.evaluator, a.svc.session, log)
	pack, err := routerpack.New()
	if err != nil {
		return fail(err)
	}
	a.httpSrv = &http.Server{Handler: pageapi.Handler(pageapi.Deps{Accounts: a.svc.evaluator, Settings: a.svc.settings, Clock: opts.Clock, Log: log}), ReadHeaderTimeout: 10 * time.Second}
	a.adminSrv = &http.Server{Handler: admin.Handler(admin.Deps{Auth: a.svc.auth, Plans: a.svc.plan, Members: a.svc.member, Batches: a.svc.voucher, Sessions: a.svc.session, Settings: a.svc.settings, Router: pack, Log: log}), ReadHeaderTimeout: 10 * time.Second}
	return a, nil
}

// sessionRepo adapts the store to session.Repo: the store's transaction type is
// concrete, the service wants its own consumer-side interface.
type sessionRepo struct{ *store.Store }

func (r sessionRepo) InAcctTx(ctx context.Context, fn func(session.AcctTx) error) error {
	return r.Store.InAcctTx(ctx, func(tx *store.AcctTx) error { return fn(tx) })
}

// seed performs first-boot seeding (O15, BR-21) and prints the generated admin
// password exactly once.
func (a *App) seed(ctx context.Context, box *secret.Box, opts Options) error {
	pw, err := auth.GeneratePassword(20)
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	kc, err := box.KeyCheck()
	if err != nil {
		return err
	}
	radiusSecret := a.cfg.RadiusSharedSecret
	if radiusSecret == "" {
		if radiusSecret, err = auth.GeneratePassword(24); err != nil {
			return err
		}
	}
	seeded, err := a.store.FirstBoot(ctx, store.SeedInput{
		AdminUsername:     a.cfg.AdminUsername,
		AdminPasswordHash: hash,
		KeyCheck:          kc,
		RadiusSecret:      radiusSecret,
		Now:               opts.Clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("first-boot seeding: %w", err)
	}
	if !seeded {
		return nil
	}
	fmt.Fprintf(opts.Stdout, "=== HotspotPlusLight first start ===\nAdmin username: %s\nAdmin password: %s\nChange it with: hotspotpluslight admin set-password\n====================================\n",
		a.cfg.AdminUsername, pw)
	a.log.Info("admin password generated at first start")
	return nil
}

// Start binds the four sockets and begins serving.
func (a *App) Start() (err error) {
	var closers []io.Closer
	defer func() {
		if err != nil {
			for _, c := range closers {
				c.Close()
			}
		}
	}()
	if a.httpLn, err = net.Listen("tcp", a.cfg.HTTPAddr); err != nil {
		return fmt.Errorf("listen HSP_HTTP_ADDR: %w", err)
	}
	closers = append(closers, a.httpLn)
	if a.adminLn, err = net.Listen("tcp", a.cfg.AdminAddr); err != nil {
		return fmt.Errorf("listen HSP_ADMIN_ADDR: %w", err)
	}
	closers = append(closers, a.adminLn)
	if a.authConn, err = net.ListenPacket("udp", a.cfg.RadiusAuthAddr); err != nil {
		return fmt.Errorf("listen HSP_RADIUS_AUTH_ADDR: %w", err)
	}
	closers = append(closers, a.authConn)
	if a.acctConn, err = net.ListenPacket("udp", a.cfg.RadiusAcctAddr); err != nil {
		return fmt.Errorf("listen HSP_RADIUS_ACCT_ADDR: %w", err)
	}
	a.addrs = Addrs{a.httpLn.Addr().String(), a.adminLn.Addr().String(), a.authConn.LocalAddr().String(), a.acctConn.LocalAddr().String()}
	go a.serveHTTP("public", a.httpSrv, a.httpLn)
	go a.serveHTTP("admin", a.adminSrv, a.adminLn)
	a.startPruning()
	a.radius.Serve(a.authConn, a.acctConn)
	a.log.Info("listening", "http", a.addrs.HTTP, "admin", a.addrs.Admin, "radius_auth", a.addrs.RadiusAuth, "radius_acct", a.addrs.RadiusAcct)
	return nil
}

// pruneEvery is how often old sessions are pruned after the run at start (O18).
const pruneEvery = 24 * time.Hour

// startPruning prunes once now, in the background so RADIUS serving never waits for it (so a restart applies a changed retention) and
// then daily until Shutdown. Real timer, not the injected clock: it only
// schedules; the cut-off itself comes from the Clock.
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

func (a *App) serveHTTP(name string, s *http.Server, ln net.Listener) {
	if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		a.log.Error("http listener stopped", "listener", name, "err", err)
	}
}

func (a *App) Addrs() Addrs { return a.addrs }

// Shutdown stops accepting, drains in-flight work, then closes the database.
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	a.stopOnce.Do(func() {
		if a.pruneCancel != nil {
			a.pruneCancel()
			<-a.pruneDone
		}
		err = errors.Join(a.httpSrv.Shutdown(ctx), a.adminSrv.Shutdown(ctx), a.radius.Shutdown(ctx), a.store.Close())
		a.log.Info("shutdown complete")
	})
	return err
}

// SetAdminPassword is the `admin set-password` subcommand: it opens the existing
// database (no migration, no seeding), applies BR-20, stores the bcrypt hash and
// deletes every admin session. The password is never printed or logged.
func SetAdminPassword(ctx context.Context, opts Options, password string) error {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Clock == nil {
		opts.Clock = clock.Real{}
	}
	cfg, err := config.Load(opts.Getenv)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(cfg.DataDir, "hsp.db")
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

// Run is the `serve` subcommand: start, wait for ctx cancellation, shut down gracefully.
func Run(ctx context.Context, opts Options) error {
	a, err := New(ctx, opts)
	if err != nil {
		return err
	}
	if err := a.Start(); err != nil {
		a.store.Close()
		return err
	}
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.Shutdown(sctx)
}
