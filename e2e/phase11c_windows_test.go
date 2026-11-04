//go:build windows

package e2e

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
)

// E11.33 Windows service smoke test: install, start, /healthz, stop, uninstall.
// It needs Administrator rights and changes the machine, so it runs only in CI
// (HSP_TEST_WINDOWS_SERVICE=1). It uses the default ports (public :8080).
func TestE11_33_WindowsService(t *testing.T) {
	if os.Getenv("HSP_TEST_WINDOWS_SERVICE") != "1" {
		t.Skip("set HSP_TEST_WINDOWS_SERVICE=1 (Administrator, changes Windows services)")
	}
	bin := harness.Binary(t)
	dir := filepath.Join(t.TempDir(), "hsp dir") // a space on purpose
	svc := func(args ...string) {
		t.Helper()
		out, err := exec.Command(bin, append([]string{"service"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("service %v: %v\n%s", args, err, out)
		}
	}
	svc("install", "--dir", dir)
	t.Cleanup(func() {
		exec.Command(bin, "service", "stop").Run()
		exec.Command(bin, "service", "uninstall").Run()
	})
	if _, err := os.Stat(filepath.Join(dir, "hsp.env")); err != nil {
		t.Fatal("hsp.env not created:", err)
	}
	svc("start")
	ok := false
	for i := 0; i < 60 && !ok; i++ {
		if r, err := http.Get("http://127.0.0.1:8080/healthz"); err == nil {
			ok = r.StatusCode == 200
			r.Body.Close()
		}
		if !ok {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !ok {
		logs, _ := os.ReadFile(filepath.Join(dir, "hsp.log"))
		t.Fatalf("/healthz not reachable; hsp.log:\n%s", logs)
	}
	svc("stop")
	logs, _ := os.ReadFile(filepath.Join(dir, "hsp.log"))
	if !strings.Contains(string(logs), "shutdown complete") {
		t.Errorf("no 'shutdown complete' in hsp.log:\n%s", logs)
	}
	svc("uninstall")
}
