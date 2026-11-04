//go:build perf

package perf

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
)

const (
	readmeStart = "<!-- perf:start -->"
	readmeEnd   = "<!-- perf:end -->"
)

func ms(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d)/float64(time.Millisecond)) }

func table(rs []Result) string {
	var b strings.Builder
	b.WriteString("| Online users | Result | Start burst packets/s | Burst p50/p95/p99 (ms) | Steady packets/s | Auth p50/p95/p99 (ms) | Accounting p50/p95/p99 (ms) | Pages p50/p95/p99 (ms) | Lost | Server CPU % | Server RSS MB | DB MB |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		res := "pass"
		if !r.Pass {
			res = "FAIL"
		}
		fmt.Fprintf(&b, "| %d | %s | %.0f | %s/%s/%s | %.0f | %s/%s/%s | %s/%s/%s | %s/%s/%s | %d | %s | %s | %.1f |\n", r.N, res,
			r.BurstPerSec, ms(r.Burst.P50), ms(r.Burst.P95), ms(r.Burst.P99), r.SteadyPerSec,
			ms(r.Auth.P50), ms(r.Auth.P95), ms(r.Auth.P99), ms(r.Acct.P50), ms(r.Acct.P95), ms(r.Acct.P99),
			ms(r.Page.P50), ms(r.Page.P95), ms(r.Page.P99), r.Lost+r.BadCode, r.CPUPct, r.RSSMB, r.DBMB)
	}
	return b.String()
}

func machine() string {
	cpu := "unknown"
	switch runtime.GOOS {
	case "darwin":
		if o, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			cpu = strings.TrimSpace(string(o))
		}
	case "linux":
		if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "model name") {
					cpu = strings.TrimSpace(l[strings.Index(l, ":")+1:])
					break
				}
			}
		}
	case "windows":
		if o, err := exec.Command("powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_Processor).Name").Output(); err == nil {
			cpu = strings.TrimSpace(string(o))
		}
	}
	return fmt.Sprintf("OS %s/%s, CPU %s, %d cores, %s", runtime.GOOS, runtime.GOARCH, cpu, runtime.NumCPU(), runtime.Version())
}

func maxOnline(rs []Result) int {
	m := 0
	for _, r := range rs {
		if r.Pass {
			m = r.N
		} else {
			break
		}
	}
	return m
}

func summary(rs []Result, cfg Config) string {
	var b strings.Builder
	date := time.Now().Format("2006-01-02")
	fmt.Fprintf(&b, "Max online users on this machine: **%d** (last passing step). Run on %s, each step %d s after its warm-up.\n\n", maxOnline(rs), date, cfg.StepSec)
	fmt.Fprintf(&b, "Machine: %s.\n\n", machine())
	b.WriteString(table(rs))
	for _, r := range rs {
		if !r.Pass {
			fmt.Fprintf(&b, "\nStep %d failed: %s.\n", r.N, strings.Join(r.Why, "; "))
		}
	}
	return b.String()
}

func joinInts(v []int) string {
	var p []string
	for _, n := range v {
		p = append(p, fmt.Sprint(n))
	}
	if len(p) < 2 {
		return strings.Join(p, "")
	}
	return strings.Join(p[:len(p)-1], ", ") + " and " + p[len(p)-1]
}

// details is the "what the test does" text, built from the steps and seconds really used.
func details(cfg Config) string {
	return fmt.Sprintf(`## What the test does

The test starts the real server as a child process with a fresh database. It plays a fake MikroTik router against it, over real UDP and HTTP.

For each step with N online users:
- N members are made in the database with one plan. Their passwords are encrypted like real ones.
- Warm-up: every user gets one Accounting Start, spread over the warm-up. The warm-up is a quarter of the step time (at least 3 s), and long enough that there are at most %d Starts per second. It is a burst, so it is reported on its own ("Start burst").
- Steady part (%d s): every user sends one Interim Update each %d seconds. So the server gets N/%d accounting packets per second.
- During the whole step, 10%% of N extra members log in (Access-Request), spread over the step.
- 1%% of the members (at least 2) are over their data quota. Half are over it before their Start (kicked at the Start). The other half cross it on their first Interim (so the Interim path is tested). The server must send a Disconnect for each. The fake router answers each Disconnect and then stops sending packets for that user.
- The admin Members page (/admin/members/) and Online sessions page (/admin/sessions/) are loaded every 3 seconds.
- The fake router uses %d workers. Each has its own UDP sockets and one request in flight, so the 256 RADIUS identifiers per socket never run out. Open loop: every packet has a planned send time. Latency is measured from the planned time, so a slow load generator or a slow server both show up.

The steps are %s users. The test stops at the first step that fails.

## A step passes when

- no RADIUS reply is lost or later than 1 second (the router itself waits 5 seconds)
- every reply has the right code: Access-Accept for logins, Accounting-Response for accounting
- the load generator keeps up: dispatch lag (real send minus planned send) is at most 1 second, and the steady packets/s is at least 95%% of the planned rate
- login (auth) p99 is below 500 ms (steady part)
- accounting p99 is below 200 ms (steady part)
- admin pages p95 is below 1 second
- there is no SQLITE_BUSY or "database is locked" in the server log
- the database shows the real work: the number of session rows ended by the server ("Server:...") equals the number of over-quota members, open sessions are about N minus those, and the sum of used download bytes matches what was sent and answered (within 0.5%%)
- (extra check) at least 90%% of the over-quota members got a Disconnect

The max online users is the last passing step. The test only measures: it does not fail the build unless you set HSP_PERF_MIN_USERS.

## Please note

- The real limit may be the MikroTik router itself, not this server. A small router may not handle thousands of Hotspot users and Interim updates.
- The fake router and the server run on the same machine and share its CPU. The server CPU and memory numbers are for the server process only.
- The CPU number is the share of one core (100 means one full core).
- Loopback has no network delay and no packet loss. A real network is slower.
- The test opens 2 UDP sockets per worker (%d here) and the server opens its own. If the test stops with "too many open files", raise the limit with `+"`ulimit -n 4096`"+` (Linux, macOS).
- If a step fails, the server log of that step is kept in bin/perf-last-fail.log.
- Run it again with `+"`make perf`"+` (full, writes this file and the README section) or `+"`make perf-quick`"+` (short, prints only). On Windows use `+"`make perf-windows`"+` and run bin/perf.test.exe with the flag -test.run TestCapacity -test.v -test.timeout 60m.
`, maxStartRate, cfg.StepSec, interimEvery, interimEvery, workers, joinInts(cfg.Steps), 2*workers)
}

// atomicWrite writes through a temp file in the same folder and renames it.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// replaceBetween swaps the text between the two markers. It fails unless each
// marker appears exactly once and the end comes after the start.
func replaceBetween(s, block string) (string, error) {
	if n := strings.Count(s, readmeStart); n != 1 {
		return "", fmt.Errorf("README: %q appears %d times, want exactly 1", readmeStart, n)
	}
	if n := strings.Count(s, readmeEnd); n != 1 {
		return "", fmt.Errorf("README: %q appears %d times, want exactly 1", readmeEnd, n)
	}
	i := strings.Index(s, readmeStart)
	j := strings.Index(s, readmeEnd)
	if j < i {
		return "", fmt.Errorf("README: the end marker is before the start marker")
	}
	return s[:i+len(readmeStart)] + "\n" + block + s[j:], nil
}

// report prints the summary. Only a full run (default steps and seconds) also
// writes docs/perf-results.md and the README section.
func report(t *testing.T, rs []Result, cfg Config) {
	sum := summary(rs, cfg)
	fmt.Println("\n" + sum)
	if !cfg.Full {
		fmt.Println("Custom run: README.md and docs/perf-results.md were not changed (only the default full run writes them).")
		return
	}
	if maxOnline(rs) == 0 {
		fmt.Println("No step passed: README.md and docs/perf-results.md were not changed (they keep the last good result).")
		return
	}
	root := harness.ModuleRoot()
	rp := filepath.Join(root, "README.md")
	b, err := os.ReadFile(rp)
	if err != nil {
		t.Errorf("README: %v", err)
		return
	}
	readme, err := replaceBetween(string(b), sum+"\nFull report and explanation: [docs/perf-results.md](docs/perf-results.md).\n")
	if err != nil {
		t.Errorf("%v", err)
		return
	}
	full := "# Capacity test results\n\n" + sum + "\n" + details(cfg)
	if err := atomicWrite(rp, []byte(readme)); err != nil {
		t.Errorf("write README: %v", err)
		return
	}
	if err := atomicWrite(filepath.Join(root, "docs", "perf-results.md"), []byte(full)); err != nil {
		t.Errorf("write perf-results.md: %v", err)
	}
}
