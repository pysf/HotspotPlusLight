package app

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdminURL(t *testing.T) {
	for in, want := range map[string]string{
		":8080":           "http://localhost:8080/admin",
		"0.0.0.0:8080":    "http://localhost:8080/admin",
		"[::]:9000":       "http://localhost:9000/admin",
		"127.0.0.1:8080":  "http://127.0.0.1:8080/admin",
		"192.168.1.10:81": "http://192.168.1.10:81/admin",
		"[fe80::1]:8080":  "http://[fe80::1]:8080/admin",
		"[::1]:8080":      "http://[::1]:8080/admin",
		"":                "http://localhost:8080/admin",
		"junk":            "http://localhost:8080/admin",
	} {
		if got := AdminURL(in); got != want {
			t.Errorf("AdminURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShouldOpenBrowser(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name string
		e    BrowserEnv
		want bool
	}{
		{"terminal on mac", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env()}, true},
		{"terminal on windows", BrowserEnv{Interactive: true, GOOS: "windows", Getenv: env()}, true},
		{"linux with DISPLAY", BrowserEnv{Interactive: true, GOOS: "linux", Getenv: env("DISPLAY", ":0")}, true},
		{"linux with Wayland", BrowserEnv{Interactive: true, GOOS: "linux", Getenv: env("WAYLAND_DISPLAY", "w0")}, true},
		{"linux without screen", BrowserEnv{Interactive: true, GOOS: "linux", Getenv: env()}, false},
		{"not a terminal", BrowserEnv{GOOS: "darwin", Getenv: env()}, false},
		{"windows service", BrowserEnv{Interactive: true, Service: true, GOOS: "windows", Getenv: env()}, false},
		{"systemd", BrowserEnv{Interactive: true, GOOS: "linux", Getenv: env("DISPLAY", ":0", "INVOCATION_ID", "x")}, false},
		{"update restart", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_RESTARTED", "1")}, false},
		{"off 0", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_OPEN_BROWSER", "0")}, false},
		{"off false", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_OPEN_BROWSER", "False")}, false},
		{"off no", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_OPEN_BROWSER", "no")}, false},
		{"off off", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_OPEN_BROWSER", "OFF")}, false},
		{"on 1", BrowserEnv{Interactive: true, GOOS: "darwin", Getenv: env("HSP_OPEN_BROWSER", "1")}, true},
	}
	for _, tc := range tests {
		if got := ShouldOpenBrowser(tc.e); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestOpenerCommand(t *testing.T) {
	const u = "http://localhost:8080/admin"
	for goos, want := range map[string]string{
		"windows": "rundll32 url.dll,FileProtocolHandler " + u,
		"darwin":  "open " + u,
		"linux":   "xdg-open " + u,
	} {
		name, args := openerCommand(goos, u)
		if got := strings.Join(append([]string{name}, args...), " "); got != want {
			t.Errorf("%s: got %q, want %q", goos, got, want)
		}
	}
}

// runWithOpener runs the app with a fake opener and returns the opened URLs and the stdout text.
func runWithOpener(t *testing.T, interactive bool, extraEnv map[string]string) ([]string, string) {
	t.Helper()
	opts, _, _ := runEnv(t)
	base := opts.Getenv
	opts.Getenv = func(k string) string {
		if k == "INVOCATION_ID" {
			return ""
		}
		if v, ok := extraEnv[k]; ok {
			return v
		}
		return base(k)
	}
	var mu sync.Mutex
	var urls []string
	var out bytes.Buffer
	opts.Stdout = &out
	opts.Interactive = interactive
	opts.OpenBrowser = func(u string) error { mu.Lock(); urls = append(urls, u); mu.Unlock(); return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opts) }()
	time.Sleep(500 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return urls, out.String()
}

func TestRunOpensTheAdminPageOnce(t *testing.T) {
	urls, out := runWithOpener(t, true, map[string]string{"DISPLAY": ":0"})
	if len(urls) != 1 || !strings.HasPrefix(urls[0], "http://127.0.0.1:") || !strings.HasSuffix(urls[0], "/admin") {
		t.Fatalf("opened %v", urls)
	}
	if !strings.Contains(out, "Admin panel: "+urls[0]) {
		t.Errorf("stdout %q lacks the admin line", out)
	}
}

func TestRunDoesNotOpenWhenNotInteractive(t *testing.T) {
	urls, out := runWithOpener(t, false, map[string]string{"DISPLAY": ":0"})
	if len(urls) != 0 || strings.Contains(out, "Admin panel:") {
		t.Fatalf("opened %v, stdout %q", urls, out)
	}
}

func TestRunDoesNotOpenWhenSwitchedOff(t *testing.T) {
	urls, out := runWithOpener(t, true, map[string]string{"DISPLAY": ":0", "HSP_OPEN_BROWSER": "0"})
	if len(urls) != 0 {
		t.Fatalf("opened %v", urls)
	}
	if !strings.Contains(out, "Admin panel:") {
		t.Errorf("stdout %q lacks the admin line", out)
	}
}
