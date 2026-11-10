package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// This file holds the systemd service commands. It has no build tag, so the
// logic is unit tested on every system. service_linux.go connects it to the real system.

const (
	systemdUnitName = "hotspotpluslight"
	systemdUnitPath = "/etc/systemd/system/hotspotpluslight.service"
	systemdUser     = "hotspotpluslight"
	systemdDefault  = "/var/lib/hotspotpluslight"
	systemdProgram  = "hotspotpluslight" // the program file name inside the install folder
)

// commander runs outside programs. Tests replace it with a fake.
type commander interface {
	LookPath(name string) (string, error)
	Run(name string, args ...string) ([]byte, error)
}

// osCommander runs real programs.
type osCommander struct{}

// LookPath finds a program in PATH.
func (osCommander) LookPath(name string) (string, error) { return exec.LookPath(name) }

// Run runs a program and returns its combined output.
func (osCommander) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// systemd holds everything the service commands touch outside this code.
type systemd struct {
	cmd        commander
	lookupUser func(name string) (*user.User, error)
	chown      func(path string, uid, gid int) error
	euid       func() int
	exe        string // the running program file, copied into the install folder
	unitPath   string
	out        io.Writer
}

// newSystemd returns a systemd helper wired to the real system.
func newSystemd(out io.Writer) (*systemd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &systemd{cmd: osCommander{}, lookupUser: user.Lookup, chown: os.Lchown, euid: os.Geteuid, exe: exe, unitPath: systemdUnitPath, out: out}, nil
}

// systemdUnitText returns the unit file for a program installed in dir.
// Exit code 75 is the self-update restart: Restart=always (and on-failure) starts the program again.
// ReadWritePaths keeps dir writable for the update swap even under /usr or /etc. There is no Group=: systemd uses the primary group of the user.
func systemdUnitText(dir string) string {
	return fmt.Sprintf(`[Unit]
Description=HotspotPlusLight
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%[2]s
WorkingDirectory=%[1]s
ExecStart=%[1]s/%[3]s --env-file %[1]s/hsp.env serve
# Exit code 75 is a self-update restart. Restart=always starts the program again.
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=full
ReadWritePaths=%[1]s
ProtectHome=yes
PrivateTmp=yes
LimitNOFILE=65536
StandardOutput=journal

[Install]
WantedBy=multi-user.target
`, dir, systemdUser, systemdProgram)
}

// envFileText returns the text of a new hsp.env with a new secret key.
func envFileText(dataDir string) (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return fmt.Sprintf("# HotspotPlus Lite settings. Keep a copy of HSP_SECRET_KEY with your backups.\nHSP_SECRET_KEY=%s\nHSP_DATA_DIR=%s\n",
		base64.StdEncoding.EncodeToString(key), dataDir), nil
}

// systemctl runs systemctl and adds its output to an error.
func (s *systemd) systemctl(args ...string) error {
	if out, err := s.cmd.Run("systemctl", args...); err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// needRoot fails with a clear message when the process is not root.
func (s *systemd) needRoot() error {
	if s.euid() != 0 {
		return errors.New("this command needs root: run it with sudo")
	}
	return nil
}

// run runs one of install, uninstall, start and stop.
func (s *systemd) run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: service install [--dir <folder>] | uninstall | start | stop")
	}
	if err := s.needRoot(); err != nil {
		return err
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		dir := fs.String("dir", systemdDefault, "folder for the program, hsp.env and data")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return s.install(*dir)
	case "uninstall":
		return s.uninstall()
	case "start":
		if err := s.systemctl("start", systemdUnitName); err != nil {
			return err
		}
		fmt.Fprintln(s.out, "Service started.")
		return nil
	case "stop":
		if err := s.systemctl("stop", systemdUnitName); err != nil {
			return err
		}
		fmt.Fprintln(s.out, "Service stopped.")
		return nil
	}
	return fmt.Errorf("unknown service command %q", args[0])
}

// serviceUser returns the service user and creates it when it is missing.
// Any lookup error other than "unknown user" stops the install. If useradd
// fails (for example because the group exists), it is tried once more with --gid.
func (s *systemd) serviceUser(dir string) (*user.User, error) {
	u, err := s.lookupUser(systemdUser)
	if err == nil {
		return u, nil
	}
	var unknown user.UnknownUserError
	if !errors.As(err, &unknown) {
		return nil, fmt.Errorf("could not look up the user %s: %w", systemdUser, err)
	}
	if _, err := s.cmd.LookPath("useradd"); err != nil {
		return nil, fmt.Errorf("the user %s does not exist and useradd was not found; create it by hand, for example: adduser --system --group --home %s --shell /usr/sbin/nologin %s", systemdUser, dir, systemdUser)
	}
	base := []string{"--system", "--home-dir", dir, "--shell", "/usr/sbin/nologin"}
	out, err := s.cmd.Run("useradd", append(append([]string{}, base...), "--user-group", systemdUser)...)
	if err != nil {
		out, err = s.cmd.Run("useradd", append(append([]string{}, base...), "--gid", systemdUser, systemdUser)...)
	}
	if err != nil {
		return nil, fmt.Errorf("could not create the user %s: %w: %s", systemdUser, err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintln(s.out, "Created the user", systemdUser)
	return s.lookupUser(systemdUser)
}

// chownTree gives the folder and everything in it to uid and gid. A symlink itself is changed, never its target (WalkDir does not follow links).
func (s *systemd) chownTree(root string, uid, gid int) error {
	return filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return s.chown(p, uid, gid)
	})
}

// install creates the user and the folder, copies the program, writes hsp.env
// when it is missing and the unit file, and enables the service.
func (s *systemd) install(dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if strings.ContainsAny(dir, " \t\n\"'\\%$") {
		return fmt.Errorf("the folder %q has characters that a unit file cannot hold; use a simple path", dir)
	}
	for _, p := range []string{"/home", "/root", "/run/user"} {
		if dir == p || strings.HasPrefix(dir, p+"/") {
			return fmt.Errorf("the folder %q is under %s, which the unit hides (ProtectHome=yes); choose another folder", dir, p)
		}
	}
	if st, err := os.Lstat(dir); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the folder %s is a symbolic link; use the real folder", dir)
	}
	if _, err := os.Lstat(s.unitPath); err == nil {
		return fmt.Errorf("service %s already exists (run `service uninstall` first)", systemdUnitName)
	}
	u, err := s.serviceUser(dir)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(dir, "data")
	// Missing parents are root-owned and open to everyone; only dir itself belongs to the service user.
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := mkdirIfMissing(dir); err != nil {
		return err
	}
	// The folder must be writable by the service user: the update renames the program file in it.
	if err := os.Chmod(dir, 0o750); err != nil {
		return err
	}
	if err := mkdirIfMissing(dataDir); err != nil {
		return err
	}
	if err := refuseSymlinks(dataDir); err != nil {
		return err
	}
	envFile := filepath.Join(dir, "hsp.env")
	switch st, err := os.Lstat(envFile); {
	case err == nil && !st.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file; remove it first", envFile)
	case err == nil:
		if err := os.Chmod(envFile, 0o600); err != nil {
			return err
		}
		fmt.Fprintln(s.out, "Kept the existing", envFile, "(its mode is now 0600)")
	case errors.Is(err, os.ErrNotExist):
		body, err := envFileText(dataDir)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(envFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(s.out, "Created", envFile)
	default:
		return err
	}
	if err := copyProgram(s.exe, filepath.Join(dir, systemdProgram)); err != nil {
		return err
	}
	if err := s.chownTree(dir, uid, gid); err != nil {
		return err
	}
	if err := os.WriteFile(s.unitPath, []byte(systemdUnitText(dir)), 0o644); err != nil {
		return err
	}
	if err := s.systemctl("daemon-reload"); err != nil {
		return err
	}
	if err := s.systemctl("enable", systemdUnitName); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "Service %s installed (starts at boot). Start it with: sudo hotspotpluslight service start\nLogs: journalctl -u %s -f\nTo reset the admin password: sudo -u %s %s/%s admin set-password --env-file %s\n",
		systemdUnitName, systemdUnitName, systemdUser, dir, systemdProgram, envFile)
	return nil
}

// mkdirIfMissing creates dir with mode 0750. An existing real folder is fine; a symlink or file is refused.
func mkdirIfMissing(dir string) error {
	st, err := os.Lstat(dir)
	switch {
	case err == nil && !st.IsDir():
		return fmt.Errorf("%s is not a folder (a symbolic link is not allowed)", dir)
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return os.Mkdir(dir, 0o750)
	}
	return err
}

// refuseSymlinks fails when root or anything under it is a symbolic link. The installer runs as root, so it never follows links.
func refuseSymlinks(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link; remove it first", p)
		}
		return nil
	})
}

// copyProgram copies the program file to dst with mode 0755. It does nothing
// when src already is dst. The copy goes to a new temporary file first, then it
// is renamed. A symlink at dst or at the temporary name is refused; O_EXCL also
// refuses a link at the temporary name.
func copyProgram(src, dst string) error {
	if st, err := os.Lstat(dst); err == nil && !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file; remove it first", dst)
	}
	if a, err := filepath.EvalSymlinks(src); err == nil {
		if b, err := filepath.EvalSymlinks(dst); err == nil && a == b {
			return os.Chmod(dst, 0o755)
		}
	}
	tmp := dst + ".tmp"
	if st, err := os.Lstat(tmp); err == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file; remove it first", tmp)
		}
		if err := os.Remove(tmp); err != nil {
			return err
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(0o755); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// uninstall stops and disables the service and removes the unit file. The data folder stays.
func (s *systemd) uninstall() error {
	if err := s.systemctl("disable", "--now", systemdUnitName); err != nil {
		return fmt.Errorf("could not stop the service, so it was not removed: %w", err)
	}
	if err := os.Remove(s.unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := s.systemctl("daemon-reload"); err != nil {
		return err
	}
	fmt.Fprintln(s.out, "Service removed. Your data folder and the user were not touched.")
	return nil
}
