//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"hotspotpluslight/internal/app"
	"hotspotpluslight/internal/update"
)

// serviceName is the Windows service name; defaultDir is the default folder for
// hsp.env, hsp.log and the data folder.
const (
	serviceName = "HotspotPlusLight"
	defaultDir  = `C:\ProgramData\HotspotPlusLight`
	stopWait    = 25 * time.Second // less than the 30 s WaitHint given to Windows
)

// isWindowsService reports whether the process was started by the Service Control Manager.
func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runAsService runs the app under the Service Control Manager and returns when
// it stops. There is no console, so logs and the first-start text go to hsp.log
// next to the env file. It fails without an env file.
func runAsService(envFile string, getenv func(string) string) error {
	if envFile == "" {
		return errors.New("the service needs --env-file")
	}
	logFile, err := os.OpenFile(filepath.Join(filepath.Dir(envFile), "hsp.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	h := &handler{opts: app.Options{Stdout: logFile, LogWriter: logFile, Getenv: getenv, Version: version, Service: true}}
	return svc.Run(serviceName, h)
}

// handler is the service handler that runs app.Run.
type handler struct{ opts app.Options }

// Execute implements svc.Handler. It reports Running only after the listeners
// are bound and handles Stop at any time, also during start. It returns a
// failure exit code (so Windows restarts the service) only if the app fails by itself.
func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	// Accepts must be set here, or Windows never delivers a Stop during a slow start.
	// The WaitHint covers a slow migration.
	status <- svc.Status{State: svc.StartPending, Accepts: svc.AcceptStop | svc.AcceptShutdown, WaitHint: 30000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ready := make(chan struct{})
	opts := h.opts
	opts.Ready = func() { close(ready) }
	go func() { done <- app.Run(ctx, opts) }()
	// Control requests are read from the start, so a Stop during a slow start works.
	// A nil ready channel never fires; it is set to nil once Running is reported.
	readyCh := (<-chan struct{})(ready)
	for {
		select {
		case <-readyCh:
			readyCh = nil
			status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
		case err := <-done:
			// The app ended by itself (for example a bad config): report failure so Windows restarts it.
			if err == nil && readyCh != nil {
				err = errors.New("the app stopped before it was ready")
			}
			if errors.Is(err, update.ErrRestart) { // an update restart: a non-zero exit makes the recovery action start the service again
				return true, update.RestartExitCode
			}
			if err != nil {
				fmt.Fprintln(h.opts.Stdout, "error:", err)
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 30000}
				cancel()
				select {
				case <-done: // the graceful shutdown ended
					return false, 0
				case <-time.After(stopWait):
					// Exit code 0: an admin Stop must never trigger a recovery restart.
					fmt.Fprintln(h.opts.Stdout, "error: the app did not stop in time")
					return false, 0
				}
			}
		}
	}
}

// serviceCommand runs one of install, uninstall, start and stop.
func serviceCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: service install [--dir <folder>] | uninstall | start | stop")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		dir := fs.String("dir", defaultDir, "folder for hsp.env, hsp.log and data")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return installService(*dir, stdout)
	case "uninstall":
		return withService(func(m *mgr.Mgr, s *mgr.Service) error {
			if err := stopService(s); err != nil {
				return fmt.Errorf("could not stop the service, so it was not removed: %w", err)
			}
			if err := s.Delete(); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "Service removed. Your data folder was not touched.")
			return nil
		})
	case "start":
		return withService(func(m *mgr.Mgr, s *mgr.Service) error {
			if err := s.Start(); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "Service started.")
			return nil
		})
	case "stop":
		return withService(func(m *mgr.Mgr, s *mgr.Service) error {
			if err := stopService(s); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "Service stopped.")
			return nil
		})
	}
	return fmt.Errorf("unknown service command %q", args[0])
}

// withService opens the installed service and calls f with it. It fails when
// not run as Administrator or when the service is not installed.
func withService(f func(*mgr.Mgr, *mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("cannot reach the service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s: %w", serviceName, err)
	}
	defer s.Close()
	return f(m, s)
}

// stopService asks the service to stop and waits up to 35 seconds, longer than
// the 30 s the service manager allows. A service that is already stopped or stopping is fine.
func stopService(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending { // already stopping: only wait
		if st, err = s.Control(svc.Stop); err != nil {
			if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) { // it stopped after the query
				return nil
			}
			return err
		}
	}
	deadline := time.Now().Add(35 * time.Second)
	for st.State != svc.Stopped {
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for the service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return err
		}
	}
	return nil
}

// installService installs the service with its data folder under dir, creates
// hsp.env with a new secret key if it is missing, and sets restart-on-failure.
func installService(dir string, stdout io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("cannot reach the service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists (run `service uninstall` first)", serviceName)
	}
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	// Only SYSTEM (S-1-5-18) and Administrators (S-1-5-32-544) may read the folder: hsp.env holds the secret key.
	// SIDs, because group names are translated on non-English Windows. A failure stops the install. VERIFY-ON-WINDOWS.
	if out, err := exec.Command("icacls", dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").CombinedOutput(); err != nil {
		return fmt.Errorf("could not restrict access to %s: %w: %s", dir, err, out)
	}
	envFile := filepath.Join(dir, "hsp.env")
	if _, err := os.Stat(envFile); errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		body := fmt.Sprintf("# HotspotPlus Lite settings. Keep a copy of HSP_SECRET_KEY with your backups.\nHSP_SECRET_KEY=%s\nHSP_DATA_DIR=%s\n",
			base64.StdEncoding.EncodeToString(key), dataDir)
		if err := os.WriteFile(envFile, []byte(body), 0o600); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Created", envFile)
	}
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "HotspotPlus Lite",
		Description: "Hotspot member manager and RADIUS server",
		StartType:   mgr.StartAutomatic,
	}, "serve", "--env-file", envFile)
	if err != nil {
		return err
	}
	defer s.Close()
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	if err == nil {
		// Also restart when the process exits with code 1 (a startup error), not only on a crash.
		err = s.SetRecoveryActionsOnNonCrashFailures(true)
	}
	if err != nil {
		fmt.Fprintln(stdout, "Warning: could not set restart-on-failure:", err)
	}
	fmt.Fprintf(stdout, "Service %s installed (starts with Windows). Start it with: hotspotpluslight service start\nLog file: %s\nTo reset the admin password: hotspotpluslight admin set-password --env-file %s\n",
		serviceName, filepath.Join(dir, "hsp.log"), envFile)
	return nil
}
