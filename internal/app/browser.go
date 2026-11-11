package app

import (
	"cmp"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"

	"hotspotpluslight/internal/config"
)

// BrowserEnv is what ShouldOpenBrowser needs to know about how the program was started.
type BrowserEnv struct {
	Interactive bool // stdout is a terminal
	Service     bool // a Windows service runs the program
	GOOS        string
	Getenv      func(string) string
}

// AdminURL returns the address to open for the admin panel, built from the bound listen address.
// A wildcard or empty host becomes localhost. A bad address gives http://localhost:8080/admin.
func AdminURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return "http://localhost:8080/admin"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/admin"
}

// ShouldOpenBrowser reports whether the program may open the admin page in a browser at start.
// It is true only for a program started by hand in a terminal, with a screen, and not restarted by an update.
func ShouldOpenBrowser(e BrowserEnv) bool {
	if !e.Interactive || e.Service || e.Getenv("INVOCATION_ID") != "" || e.Getenv("HSP_RESTARTED") != "" {
		return false
	}
	if config.OpenBrowserOff(e.Getenv) {
		return false
	}
	if e.GOOS == "linux" && e.Getenv("DISPLAY") == "" && e.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return true
}

// openerCommand returns the program and arguments that open url in the default browser on goos.
// The URL is one argument and never goes through a shell.
func openerCommand(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

// SystemOpener opens url in the default browser. It does not wait for the browser.
func SystemOpener(url string) error {
	name, args := openerCommand(runtime.GOOS, url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reaps the child
	return nil
}

// announceAdmin prints the admin address when the program runs in a terminal and, if allowed, opens it
// in a browser. Opening never fails the start: an error is only logged.
func (a *App) announceAdmin(opts Options) {
	url := AdminURL(a.Addrs().Admin)
	if opts.Interactive {
		fmt.Fprintf(cmp.Or[io.Writer](opts.Stdout, os.Stdout), "Admin panel: %s\n", url)
	}
	if opts.OpenBrowser == nil || !ShouldOpenBrowser(BrowserEnv{Interactive: opts.Interactive, Service: opts.Service, GOOS: runtime.GOOS, Getenv: opts.Getenv}) {
		return
	}
	if err := opts.OpenBrowser(url); err != nil {
		a.log.Debug("cannot open the browser", "err", err)
	}
}
