//go:build perf

// Package perf is the capacity test (Phase 11d). It is built only with the tag
// "perf", so normal `go test ./...` never runs it. Run it with `make perf`.
//
// It starts the real binary as a child process, plays a fake router against it
// (RADIUS over UDP, the Disconnect listener, the admin pages) and raises the
// number of online users step by step until a step fails.
package perf

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"layeh.com/radius"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/secret"

	_ "modernc.org/sqlite"
)

const (
	radiusSecret = "perf-shared-secret"
	memberPass   = "perfpass1"
	adminPass    = "Perf-Admin-Pass-42"
	maxReplyWait = time.Second
	workers      = 128
)

var defaultSteps = []int{500, 1000, 2000, 5000, 10000, 20000, 30000, 40000, 50000}

const (
	defaultStepSec = 120
	interimEvery   = 60 // seconds between Interim Updates of one user
	maxDispatchLag = time.Second
	minRateRatio   = 0.95
)

// Config is what one run really uses. The report is written from it.
type Config struct {
	Steps   []int
	StepSec int
	Full    bool // default steps and default step seconds: only then the files are written
}

// maxStartRate is the most Accounting Starts per second the warm-up sends.
const maxStartRate = 500

// warmSec is the length of the warm-up (the Start burst) of a step: a quarter
// of the step time (at least 3 s), and long enough that Starts come at most
// maxStartRate per second.
func warmSec(n, stepSec int) int { return max(3, stepSec/4, (n+maxStartRate-1)/maxStartRate) }

// Limits (the pass rules).
const (
	authP99Max = 500 * time.Millisecond
	acctP99Max = 200 * time.Millisecond
	pageP95Max = time.Second
)

// Result is one step.
type Result struct {
	N                   int
	Pass                bool
	Why                 []string
	BurstPerSec         float64 // packets/s during the warm-up (Start burst)
	SteadyPerSec        float64 // packets/s after the warm-up
	Burst               Pcts    // all packets sent during the warm-up
	Auth, Acct, Page    Pcts    // steady part only; measured from the planned send time
	AuthN, AcctN, PageN int
	MaxLag              time.Duration // worst dispatch lag (actual send minus planned send)
	Lost, BadCode       int
	Disconnects, Over   int
	ServerEnded         int // session rows with terminate_cause LIKE 'Server:%'
	OpenSessions        int
	CPUPct              string
	RSSMB               string
	DBMB                float64
}

// Pcts are latency percentiles.
type Pcts struct{ P50, P95, P99 time.Duration }

func pcts(d []time.Duration) Pcts {
	if len(d) == 0 {
		return Pcts{}
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	at := func(q float64) time.Duration { return d[min(len(d)-1, int(q*float64(len(d))))] }
	return Pcts{at(0.50), at(0.95), at(0.99)}
}

func TestCapacity(t *testing.T) {
	cfg := Config{Steps: defaultSteps, StepSec: defaultStepSec}
	if v := os.Getenv("HSP_PERF_STEPS"); v != "" {
		cfg.Steps = nil
		for _, f := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil || n < 1 {
				t.Fatalf("HSP_PERF_STEPS: bad number %q", f)
			}
			cfg.Steps = append(cfg.Steps, n)
		}
	}
	if v := os.Getenv("HSP_PERF_STEP_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 5 {
			t.Fatalf("HSP_PERF_STEP_SECONDS: need a number >= 5")
		}
		cfg.StepSec = n
	}
	floor := 0
	if v := os.Getenv("HSP_PERF_MIN_USERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("HSP_PERF_MIN_USERS: need a number >= 1")
		}
		floor = n
	}
	cfg.Full = slices.Equal(cfg.Steps, defaultSteps) && cfg.StepSec == defaultStepSec
	bin := binaryPath(t)
	var results []Result
	for _, n := range cfg.Steps {
		t.Logf("step N=%d (%d s) ...", n, cfg.StepSec)
		r := runStep(t, bin, n, cfg.StepSec)
		results = append(results, r)
		t.Logf("step N=%d pass=%v %v", n, r.Pass, r.Why)
		if !r.Pass {
			break
		}
	}
	report(t, results, cfg)
	// This is a measurement tool: a failing step only ends the run and is
	// reported. The test fails only for a setup error, or when the optional
	// floor HSP_PERF_MIN_USERS is set and the max online users is below it.
	if floor > 0 && maxOnline(results) < floor {
		t.Fatalf("max online users %d is below HSP_PERF_MIN_USERS=%d", maxOnline(results), floor)
	}
}

// binaryPath: HSP_PERF_BIN, or a binary next to the test executable, or a fresh go build.
func binaryPath(t *testing.T) string {
	if p := os.Getenv("HSP_PERF_BIN"); p != "" {
		return p
	}
	name := "hotspotpluslight"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return harness.Binary(t)
}

func freePorts(t *testing.T) (httpAddr, adminAddr, authAddr, acctAddr string) {
	var out [4]string
	for i := range out {
		if i < 2 {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			out[i] = l.Addr().String()
			l.Close()
		} else {
			l, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			out[i] = l.LocalAddr().String()
			l.Close()
		}
	}
	return out[0], out[1], out[2], out[3]
}

type child struct {
	cmd    *exec.Cmd
	logf   string
	dir    string
	admin  string
	auth   string
	acct   string
	key    string
	exited chan struct{}
}

func startChild(t *testing.T, bin string) *child {
	dir, err := os.MkdirTemp("", "hsp-perf-")
	if err != nil {
		t.Fatal(err)
	}
	httpA, adminA, authA, acctA := freePorts(t)
	key := harness.NewKey()
	logf := filepath.Join(dir, "server.log")
	lf, err := os.Create(logf)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "serve")
	cmd.Env = append(os.Environ(), "HSP_DATA_DIR="+dir, "HSP_SECRET_KEY="+key,
		"HSP_HTTP_ADDR="+httpA, "HSP_ADMIN_ADDR="+adminA, "HSP_RADIUS_AUTH_ADDR="+authA, "HSP_RADIUS_ACCT_ADDR="+acctA,
		"HSP_RADIUS_SHARED_SECRET="+radiusSecret)
	cmd.Stdout, cmd.Stderr = lf, lf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &child{cmd: cmd, logf: logf, dir: dir, admin: adminA, auth: authA, acct: acctA, key: key, exited: make(chan struct{})}
	go func() { cmd.Wait(); lf.Close(); close(c.exited) }()
	for i := 0; i < 200; i++ {
		if r, err := http.Get("http://" + httpA + "/healthz"); err == nil {
			r.Body.Close()
			return c
		}
		select {
		case <-c.exited:
			b, _ := os.ReadFile(logf)
			t.Fatalf("server exited at start: %s", b)
		case <-time.After(100 * time.Millisecond):
		}
	}
	c.stop()
	t.Fatal("server did not start")
	return nil
}

func (c *child) stop() {
	select {
	case <-c.exited:
		return
	default:
	}
	if runtime.GOOS == "windows" {
		c.cmd.Process.Kill()
	} else {
		c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.exited:
	case <-time.After(10 * time.Second):
		c.cmd.Process.Kill()
		<-c.exited
	}
}

func uname(i int) string { return "u" + strconv.Itoa(i) }

// seed puts one plan and the members into the live database in one transaction.
// Members 0..n-1 are the online users (the first `over` of them are over quota, see presetUsed);
// members n..n+n/10-1 only log in. Passwords share one encrypted blob.
func seed(t *testing.T, c *child, n, over int) {
	k, err := secret.ParseKey(c.key)
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.NewBox(k)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := box.Encrypt([]byte(memberPass))
	if err != nil {
		t.Fatal(err)
	}
	db, err := openDB(c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Unix()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	must := func(_ sql.Result, err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tx.Exec(`INSERT INTO plan(name,duration_days,speed_value,speed_unit,bulk_value,bulk_unit,time_limit_minutes,max_sessions,created_at,updated_at)
		VALUES ('perf',30,2,'Mbps',10,'GB',0,1,?,?)`, now, now))
	st, err := tx.Prepare(`INSERT INTO member(username,password_enc,plan_id,subscription_date,used_download_bytes,created_at,updated_at) VALUES (?,?,1,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	total := n + n/10
	for i := 0; i < total; i++ {
		used := int64(0)
		if i < n {
			used = presetUsed(i, over)
		}
		if _, err := st.Exec(uname(i), blob, now, used, now, now); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	must(tx.Exec(`UPDATE admin SET must_change_password=0, password_hash=? WHERE id=1`, mustHash()))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func mustHash() string {
	h, err := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}

// ---- load events ----

type kind int

const (
	evStart kind = iota
	evInterim
	evAuth
)

type event struct {
	at   time.Duration // planned send time, since the step began
	kind kind
	user int
	seq  int // interim number
}

type sample struct {
	kind    kind
	steady  bool          // planned after the warm-up
	lag     time.Duration // actual send minus planned send
	d       time.Duration // reply time minus PLANNED send time (open loop)
	lost    bool
	badCode bool
}

// worker owns one UDP socket per target port; it has one request in flight, so
// the 256 RADIUS identifiers of a socket can never run out.
type worker struct {
	auth, acct net.Conn
	id         [2]uint8
	buf        []byte
}

// exchange sends p and waits for the reply with the same identifier. It returns
// the send time, the reply time and whether the reply was lost, plus the reply code.
func (w *worker) exchange(conn net.Conn, idx int, p *radius.Packet) (sent, got time.Time, code radius.Code, lost bool) {
	w.id[idx]++
	p.Identifier = w.id[idx]
	wire, err := p.Encode()
	sent = time.Now()
	if err != nil {
		return sent, sent, 0, true
	}
	conn.SetDeadline(sent.Add(maxReplyWait))
	if _, err := conn.Write(wire); err != nil {
		return sent, sent, 0, true
	}
	for {
		n, err := conn.Read(w.buf)
		if err != nil {
			return sent, sent.Add(maxReplyWait), 0, true
		}
		if n >= 2 && w.buf[1] == p.Identifier {
			return sent, time.Now(), radius.Code(w.buf[0]), false
		}
		// an old reply to an earlier (lost) request: ignore
	}
}

// newWorkers opens all sockets in the calling goroutine, so a failure (for
// example "too many open files": see `ulimit -n`) stops the test with t.Fatalf.
func newWorkers(t *testing.T, c *child, count int) []*worker {
	var ws []*worker
	t.Cleanup(func() { // sockets of workers that never started (t.Fatalf)
		for _, w := range ws {
			w.auth.Close()
			w.acct.Close()
		}
	})
	closeAll := func() {
		for _, w := range ws {
			w.auth.Close()
			w.acct.Close()
		}
	}
	for i := 0; i < count; i++ {
		a, err := net.Dial("udp", c.auth)
		if err != nil {
			closeAll()
			t.Fatalf("open UDP socket (try a higher `ulimit -n`): %v", err)
		}
		b, err := net.Dial("udp", c.acct)
		if err != nil {
			a.Close()
			closeAll()
			t.Fatalf("open UDP socket (try a higher `ulimit -n`): %v", err)
		}
		ws = append(ws, &worker{auth: a, acct: b, buf: make([]byte, 4096)})
	}
	return ws
}

type pageStat struct {
	mu   sync.Mutex
	d    []time.Duration
	fail int
}

// Per-user numbers of the load, so the end state can be checked.
const (
	interimDown = 500_000
	interimUp   = 50_000
	overStart   = 20 << 30 // used bytes of a member who is over quota before the Start
	overMargin  = 100_000  // a member who crosses the quota on the first Interim is this far under
	planBulk    = 10 << 30 // the plan's data limit (10 GB)
)

// presetUsed is the used download bytes a member has before the test.
// The first `over` members are over quota: even ones already (they get kicked at
// the Start), odd ones cross the limit on their first Interim.
func presetUsed(i, over int) int64 {
	switch {
	case i >= over:
		return 0
	case i%2 == 0:
		return overStart
	default:
		return planBulk - overMargin
	}
}

func runStep(t *testing.T, bin string, n, stepSec int) Result {
	res := Result{N: n}
	c := startChild(t, bin)
	defer func() {
		c.stop()
		if !res.Pass {
			keepLog(c.logf, n)
		}
		os.RemoveAll(c.dir)
	}()

	over := max(2, n/100)
	seed(t, c, n, over)
	coa := fakenas.ListenCoA(t, radiusSecret)
	var gone = make([]atomic.Bool, n)
	var discCount atomic.Int64
	coa.SetBehavior(func(_ int, d fakenas.Disconnect) fakenas.Reply {
		if s := radiusUser(d.Packet); strings.HasPrefix(s, "u") {
			if i, err := strconv.Atoi(s[1:]); err == nil && i < n {
				gone[i].Store(true)
			}
		}
		discCount.Add(1)
		return fakenas.ACK
	})
	db, err := openDB(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE settings SET coa_port=?`, coa.Port()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// events
	warm := warmSec(n, stepSec)
	total := time.Duration(warm+stepSec) * time.Second
	var evs []event
	expectKick := 0 // over-quota members who must be kicked
	for i := 0; i < n; i++ {
		evs = append(evs, event{at: time.Duration(float64(warm) * float64(time.Second) * float64(i) / float64(n) * 0.95), kind: evStart, user: i})
		off := 1 + float64(i%6000)/6000*59 // 1..60 s after the warm-up, spread evenly
		first := true
		for k := 0; ; k++ {
			at := time.Duration((float64(warm) + off + interimEvery*float64(k)) * float64(time.Second))
			if at >= total {
				break
			}
			if first && i < over && i%2 == 1 {
				expectKick++ // its first Interim is inside the step
			}
			first = false
			evs = append(evs, event{at: at, kind: evInterim, user: i, seq: k + 1})
		}
	}
	for i := 0; i < over; i += 2 {
		expectKick++ // kicked at the Start
	}
	for j := 0; j < n/10; j++ {
		evs = append(evs, event{at: time.Duration(float64(total) * float64(j) / float64(n/10) * 0.98), kind: evAuth, user: n + j})
	}
	sort.Slice(evs, func(a, b int) bool { return evs[a].at < evs[b].at })
	warmDur := time.Duration(warm) * time.Second
	plannedSteady := 0
	for _, ev := range evs {
		if ev.at >= warmDur {
			plannedSteady++
		}
	}

	cpu := startSampler(c.cmd.Process.Pid)
	t.Cleanup(cpu.halt) // also stops the goroutine when newWorkers calls t.Fatalf
	ws := newWorkers(t, c, workers)
	ch := make(chan event, 4096)
	lastOK := make([]atomic.Int32, n) // highest Interim number answered for each user
	var skippedSteady atomic.Int64
	var mu sync.Mutex
	var samples []sample
	var wg sync.WaitGroup
	begin := time.Now()
	for _, wk := range ws {
		wg.Add(1)
		go func(wk *worker) {
			defer wg.Done()
			defer wk.auth.Close()
			defer wk.acct.Close()
			nas := &fakenas.NAS{Secret: radiusSecret}
			var local []sample
			for ev := range ch {
				steady := ev.at >= warmDur
				if ev.kind != evAuth && gone[ev.user].Load() {
					if steady {
						skippedSteady.Add(1)
					}
					continue
				}
				var sent, got time.Time
				var code radius.Code
				var lost bool
				want := radius.CodeAccountingResponse
				switch ev.kind {
				case evAuth:
					want = radius.CodeAccessAccept
					sent, got, code, lost = wk.exchange(wk.auth, 0, nas.PAPPacket(uname(ev.user), memberPass))
				default:
					a := fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: uname(ev.user), SessionID: "s" + strconv.Itoa(ev.user),
						FramedIP: "10.1." + strconv.Itoa(ev.user/250%250) + "." + strconv.Itoa(ev.user%250+1), MAC: fmt.Sprintf("AA:BB:CC:%02X:%02X:%02X", ev.user>>16&255, ev.user>>8&255, ev.user&255)}
					if ev.kind == evInterim {
						a.Type = rfc2866.AcctStatusType_Value_InterimUpdate
						a.Seconds = uint32(interimEvery * ev.seq)
						a.Download = uint64(ev.seq) * interimDown
						a.Upload = uint64(ev.seq) * interimUp
					}
					sent, got, code, lost = wk.exchange(wk.acct, 1, nas.AcctPacket(a))
				}
				planned := begin.Add(ev.at)
				sm := sample{kind: ev.kind, steady: steady, lag: sent.Sub(planned), d: got.Sub(planned), lost: lost, badCode: !lost && code != want}
				if !lost && !sm.badCode && ev.kind == evInterim {
					lastOK[ev.user].Store(int32(ev.seq))
				}
				local = append(local, sm)
			}
			mu.Lock()
			samples = append(samples, local...)
			mu.Unlock()
		}(wk)
	}

	// admin pages
	ps := &pageStat{}
	stopPages := make(chan struct{})
	var pw sync.WaitGroup
	hc := adminClient(t, c)
	pw.Add(1)
	go func() {
		defer pw.Done()
		paths := []string{"/admin/members/", "/admin/sessions/"} // Members list and Online sessions list
		for i := 0; ; i++ {
			select {
			case <-stopPages:
				return
			case <-time.After(3 * time.Second):
			}
			s := time.Now()
			r, err := hc.Get("http://" + c.admin + paths[i%2])
			ok := err == nil
			if ok {
				buf := make([]byte, 1<<16)
				for {
					_, e := r.Body.Read(buf)
					if e != nil {
						break
					}
				}
				r.Body.Close()
				ok = r.StatusCode == 200
			}
			ps.mu.Lock()
			if ok {
				ps.d = append(ps.d, time.Since(s))
			} else {
				ps.fail++
			}
			ps.mu.Unlock()
		}
	}()

	for _, ev := range evs {
		if wait := ev.at - time.Since(begin); wait > 2*time.Millisecond {
			time.Sleep(wait)
		}
		ch <- ev
	}
	close(ch)
	wg.Wait()
	close(stopPages)
	pw.Wait()
	res.CPUPct, res.RSSMB = cpu.stop()
	time.Sleep(500 * time.Millisecond)

	var auth, acct, burst []time.Duration
	steadyDone := 0
	for _, s := range samples {
		res.MaxLag = max(res.MaxLag, s.lag)
		if s.lost {
			res.Lost++
			continue
		}
		if s.badCode {
			res.BadCode++
			continue
		}
		switch {
		case !s.steady:
			burst = append(burst, s.d)
		case s.kind == evAuth:
			auth = append(auth, s.d)
			steadyDone++
		default:
			acct = append(acct, s.d)
			steadyDone++
		}
	}
	burstN := len(burst)
	res.AuthN, res.AcctN = len(auth), len(acct)
	res.Auth, res.Acct, res.Burst = pcts(auth), pcts(acct), pcts(burst)
	res.BurstPerSec = float64(burstN) / warmDur.Seconds()
	res.SteadyPerSec = float64(steadyDone) / float64(stepSec)
	plannedPackets := plannedSteady - int(skippedSteady.Load())
	ps.mu.Lock()
	res.Page, res.PageN = pcts(ps.d), len(ps.d)
	pageFail := ps.fail
	ps.mu.Unlock()
	res.Disconnects, res.Over = int(discCount.Load()), over
	if fi, err := os.Stat(filepath.Join(c.dir, "hsp.db")); err == nil {
		sz := fi.Size()
		if w, err := os.Stat(filepath.Join(c.dir, "hsp.db-wal")); err == nil {
			sz += w.Size()
		}
		res.DBMB = float64(sz) / 1e6
	}

	// What the server really stored.
	var sumDown, wantDown int64
	var dbErr error
	if db, err := openDB(c); err != nil {
		dbErr = err
	} else {
		// Disconnects are sent after the reply, so give the last ones a moment.
		for try := 0; try < 50; try++ {
			db.QueryRow(`SELECT count(*) FROM session WHERE terminate_cause LIKE 'Server:%'`).Scan(&res.ServerEnded)
			if res.ServerEnded >= expectKick {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		dbErr = db.QueryRow(`SELECT count(*) FROM session WHERE ended_at IS NULL`).Scan(&res.OpenSessions)
		if dbErr == nil {
			dbErr = db.QueryRow(`SELECT COALESCE(SUM(used_download_bytes),0) FROM member WHERE username GLOB 'u[0-9]*' AND CAST(substr(username, 2) AS INTEGER) < ?`, n).Scan(&sumDown)
		}
		db.Close()
	}
	for i := 0; i < n; i++ {
		wantDown += presetUsed(i, over) + int64(lastOK[i].Load())*interimDown
	}

	// pass rules (all on the steady part)
	res.Pass = true
	fail := func(f string, a ...any) { res.Pass = false; res.Why = append(res.Why, fmt.Sprintf(f, a...)) }
	if res.Lost > 0 {
		fail("%d replies lost or later than 1 s", res.Lost)
	}
	if res.BadCode > 0 {
		fail("%d replies with a wrong code (want Access-Accept for logins, Accounting-Response for accounting)", res.BadCode)
	}
	if res.MaxLag > maxDispatchLag {
		fail("the load generator itself fell behind: dispatch lag %s > %s", res.MaxLag.Round(time.Millisecond), maxDispatchLag)
	}
	if plannedPackets > 0 && float64(steadyDone) < minRateRatio*float64(plannedPackets) {
		fail("steady rate %.0f packets/s is below %.0f%% of the planned %.0f", res.SteadyPerSec, minRateRatio*100, float64(plannedPackets)/float64(stepSec))
	}
	if res.Auth.P99 >= authP99Max {
		fail("auth p99 %s >= %s", res.Auth.P99, authP99Max)
	}
	if res.Acct.P99 >= acctP99Max {
		fail("accounting p99 %s >= %s", res.Acct.P99, acctP99Max)
	}
	if res.Page.P95 >= pageP95Max || pageFail > 0 {
		fail("admin pages p95 %s (failed %d)", res.Page.P95, pageFail)
	}
	if res.PageN == 0 {
		fail("no admin page was measured")
	}
	if b, _ := os.ReadFile(c.logf); strings.Contains(string(b), "SQLITE_BUSY") || strings.Contains(string(b), "database is locked") {
		fail("SQLITE_BUSY / database is locked in the server log")
	}
	if dbErr != nil {
		fail("could not read the database: %v", dbErr)
	} else {
		if res.ServerEnded != expectKick {
			fail("%d session rows ended by the server (Server:...), expected %d over-quota members", res.ServerEnded, expectKick)
		}
		if res.Disconnects < expectKick*9/10 {
			fail("only %d of %d over-quota members got a Disconnect", res.Disconnects, expectKick)
		}
		wantOpen := n - expectKick
		if tol := max(2, n/1000); res.OpenSessions < wantOpen-tol || res.OpenSessions > wantOpen+tol {
			fail("%d open sessions, expected about %d", res.OpenSessions, wantOpen)
		}
		if tol := wantDown / 200; sumDown < wantDown-tol || sumDown > wantDown+tol {
			fail("used download bytes in the database %d, expected about %d (sent and answered)", sumDown, wantDown)
		}
	}
	return res
}

// keepLog copies the server log of a failed step to bin/perf-last-fail.log (git-ignored).
func keepLog(src string, n int) {
	b, err := os.ReadFile(src)
	if err != nil {
		return
	}
	dst := filepath.Join(harness.ModuleRoot(), "bin", "perf-last-fail.log")
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if os.WriteFile(dst, []byte(fmt.Sprintf("# server log of the failed step N=%d\n", n)+string(b)), 0o644) == nil {
		fmt.Println("server log of the failed step kept in", dst)
	}
}

func openDB(c *child) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(c.dir, "hsp.db"))+"?_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func radiusUser(p *radius.Packet) string {
	if a, ok := p.Lookup(1); ok { // User-Name
		return string(a)
	}
	return ""
}

func adminClient(t *testing.T, c *child) *http.Client {
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := hc.PostForm("http://"+c.admin+"/admin/login", url.Values{"username": {"admin"}, "password": {adminPass}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin login: %d", r.StatusCode)
	}
	return hc
}

// ---- CPU and memory of the child ----

type sampler struct {
	pid     int
	stopC   chan struct{}
	done    chan struct{}
	cpu0    float64
	t0      time.Time
	cpu1    float64
	t1      time.Time
	peakRSS float64
	haveCPU bool
	haveRSS bool
	once    sync.Once
}

// halt stops the sampling goroutine (safe to call more than once).
func (s *sampler) halt() {
	s.once.Do(func() { close(s.stopC) })
	<-s.done
}

func startSampler(pid int) *sampler {
	s := &sampler{pid: pid, stopC: make(chan struct{}), done: make(chan struct{})}
	s.t0 = time.Now()
	if cpu, rss, ok := readProc(pid); ok {
		s.cpu0, s.haveCPU = cpu, true
		s.peakRSS, s.haveRSS = rss, true
	}
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.stopC:
				return
			case <-time.After(2 * time.Second):
			}
			if _, rss, ok := readProc(pid); ok {
				s.peakRSS = max(s.peakRSS, rss)
				s.haveRSS = true
			}
		}
	}()
	return s
}

func (s *sampler) stop() (cpuPct, rssMB string) {
	cpu, rss, ok := readProc(s.pid)
	s.t1 = time.Now()
	s.halt()
	if !ok || !s.haveCPU {
		return "n/a", "n/a"
	}
	s.peakRSS = max(s.peakRSS, rss)
	pct := (cpu - s.cpu0) / s.t1.Sub(s.t0).Seconds() * 100
	return fmt.Sprintf("%.0f", pct), fmt.Sprintf("%.0f", s.peakRSS/1e6)
}

// readProc returns total CPU seconds and RSS bytes of a process. Best effort per OS.
func readProc(pid int) (cpu, rss float64, ok bool) {
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return
		}
		s := string(b)
		f := strings.Fields(s[strings.LastIndex(s, ")")+2:])
		if len(f) < 22 {
			return
		}
		ut, _ := strconv.ParseFloat(f[11], 64)
		st, _ := strconv.ParseFloat(f[12], 64)
		pages, _ := strconv.ParseFloat(f[21], 64)
		return (ut + st) / 100, pages * float64(os.Getpagesize()), true
	case "windows":
		out, err := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("$p=Get-Process -Id %d; \"$($p.CPU) $($p.WorkingSet64)\"", pid)).Output()
		if err != nil {
			return
		}
		f := strings.Fields(strings.ReplaceAll(string(out), ",", "."))
		if len(f) != 2 {
			return
		}
		c, e1 := strconv.ParseFloat(f[0], 64)
		r, e2 := strconv.ParseFloat(f[1], 64)
		return c, r, e1 == nil && e2 == nil
	default: // darwin, bsd: ps gives cumulative CPU time and RSS in KB
		out, err := exec.Command("ps", "-o", "time=,rss=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return
		}
		f := strings.Fields(string(out))
		if len(f) != 2 {
			return
		}
		secs := 0.0
		days := 0.0
		clock := f[0]
		if d, rest, found := strings.Cut(clock, "-"); found { // D-HH:MM:SS after a day
			days, _ = strconv.ParseFloat(d, 64)
			clock = rest
		}
		for _, part := range strings.Split(clock, ":") {
			v, _ := strconv.ParseFloat(part, 64)
			secs = secs*60 + v
		}
		secs += days * 86400
		kb, _ := strconv.ParseFloat(f[1], 64)
		return secs, kb * 1024, true
	}
}
