package main

import (
	"bytes"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeCmd struct {
	calls     []string
	noUseradd bool
	failOn    string
	onUseradd func()
}

func (f *fakeCmd) LookPath(name string) (string, error) {
	if name == "useradd" && f.noUseradd {
		return "", errors.New("not found")
	}
	return "/usr/sbin/" + name, nil
}

func (f *fakeCmd) Run(name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if f.failOn != "" && strings.Contains(call, f.failOn) {
		return []byte("boom"), errors.New("failed")
	}
	if name == "useradd" && f.onUseradd != nil {
		f.onUseradd()
	}
	return nil, nil
}

type chownCall struct {
	path     string
	uid, gid int
}

type sdFixture struct {
	s      *systemd
	cmd    *fakeCmd
	out    *bytes.Buffer
	dir    string
	chowns []chownCall
	exists bool // the service user exists
}

func newSdFixture(t *testing.T) *sdFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Linux install uses Unix paths and modes")
	}
	tmp := t.TempDir()
	exe := filepath.Join(tmp, "src", "hsp-bin")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("program"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &sdFixture{cmd: &fakeCmd{}, out: &bytes.Buffer{}, dir: filepath.Join(tmp, "install")}
	f.cmd.onUseradd = func() { f.exists = true }
	f.s = &systemd{
		cmd: f.cmd,
		lookupUser: func(name string) (*user.User, error) {
			if !f.exists {
				return nil, user.UnknownUserError(name)
			}
			return &user.User{Uid: "991", Gid: "992", Username: name}, nil
		},
		chown: func(p string, uid, gid int) error { f.chowns = append(f.chowns, chownCall{p, uid, gid}); return nil },
		euid:  func() int { return 0 },
		exe:   exe, unitPath: filepath.Join(tmp, "unit.service"), out: f.out,
	}
	return f
}

func TestSystemdUnitText(t *testing.T) {
	want := `[Unit]
Description=HotspotPlusLight
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=hotspotpluslight
WorkingDirectory=/var/lib/hotspotpluslight
ExecStart=/var/lib/hotspotpluslight/hotspotpluslight --env-file /var/lib/hotspotpluslight/hsp.env serve
# Exit code 75 is a self-update restart. Restart=always starts the program again.
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=full
ReadWritePaths=/var/lib/hotspotpluslight
ProtectHome=yes
PrivateTmp=yes
LimitNOFILE=65536
StandardOutput=journal

[Install]
WantedBy=multi-user.target
`
	if got := systemdUnitText("/var/lib/hotspotpluslight"); got != want {
		t.Errorf("unit text differs:\n%s", got)
	}
}

func TestSystemdInstall(t *testing.T) {
	f := newSdFixture(t)
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	wantCmds := []string{
		"useradd --system --home-dir " + f.dir + " --shell /usr/sbin/nologin --user-group hotspotpluslight",
		"systemctl daemon-reload",
		"systemctl enable hotspotpluslight",
	}
	if strings.Join(f.cmd.calls, "\n") != strings.Join(wantCmds, "\n") {
		t.Errorf("commands:\n%s", strings.Join(f.cmd.calls, "\n"))
	}
	unit, err := os.ReadFile(f.s.unitPath)
	if err != nil || string(unit) != systemdUnitText(f.dir) {
		t.Errorf("unit file wrong: %v", err)
	}
	env, _ := os.ReadFile(filepath.Join(f.dir, "hsp.env"))
	if !strings.Contains(string(env), "HSP_SECRET_KEY=") || !strings.Contains(string(env), "HSP_DATA_DIR="+filepath.Join(f.dir, "data")) {
		t.Errorf("env file: %s", env)
	}
	if st, _ := os.Stat(filepath.Join(f.dir, "hsp.env")); st.Mode().Perm() != 0o600 {
		t.Errorf("env mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(f.dir); st.Mode().Perm() != 0o750 {
		t.Errorf("folder mode %v", st.Mode().Perm())
	}
	prog := filepath.Join(f.dir, "hotspotpluslight")
	if b, _ := os.ReadFile(prog); string(b) != "program" {
		t.Error("program not copied")
	}
	if st, _ := os.Stat(prog); st.Mode().Perm() != 0o755 {
		t.Errorf("program mode %v", st.Mode().Perm())
	}
	owned := map[string]bool{}
	for _, c := range f.chowns {
		if c.uid != 991 || c.gid != 992 {
			t.Errorf("chown %v", c)
		}
		owned[c.path] = true
	}
	for _, p := range []string{f.dir, filepath.Join(f.dir, "data"), filepath.Join(f.dir, "hsp.env"), prog} {
		if !owned[p] {
			t.Errorf("%s was not chowned", p)
		}
	}
	if !strings.Contains(f.out.String(), "journalctl -u hotspotpluslight -f") {
		t.Errorf("output lacks the log hint: %s", f.out)
	}
}

func TestSystemdInstallKeepsEnvAndUser(t *testing.T) {
	f := newSdFixture(t)
	f.exists = true
	if err := os.MkdirAll(f.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(f.dir, "hsp.env")
	if err := os.WriteFile(envFile, []byte("HSP_SECRET_KEY=mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(envFile); string(b) != "HSP_SECRET_KEY=mine\n" {
		t.Errorf("env file was changed: %s", b)
	}
	for _, c := range f.cmd.calls {
		if strings.HasPrefix(c, "useradd") {
			t.Error("useradd ran for an existing user")
		}
	}
}

func TestSystemdInstallNeedsRoot(t *testing.T) {
	f := newSdFixture(t)
	f.s.euid = func() int { return 1000 }
	for _, c := range []string{"install", "uninstall", "start", "stop"} {
		err := f.s.run([]string{c})
		if err == nil || !strings.Contains(err.Error(), "sudo") {
			t.Errorf("%s: %v", c, err)
		}
	}
	if len(f.cmd.calls) != 0 {
		t.Error("commands ran without root")
	}
}

func TestSystemdInstallNoUseradd(t *testing.T) {
	f := newSdFixture(t)
	f.cmd.noUseradd = true
	err := f.s.run([]string{"install", "--dir", f.dir})
	if err == nil || !strings.Contains(err.Error(), "create it by hand") {
		t.Fatalf("error: %v", err)
	}
	if _, err := os.Stat(f.s.unitPath); err == nil {
		t.Error("unit file written")
	}
}

func TestSystemdInstallRefusesSecondInstall(t *testing.T) {
	f := newSdFixture(t)
	if err := os.WriteFile(f.s.unitPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.s.run([]string{"install", "--dir", f.dir}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error: %v", err)
	}
}

func TestSystemdInstallBadDir(t *testing.T) {
	f := newSdFixture(t)
	if err := f.s.run([]string{"install", "--dir", "/tmp/a b"}); err == nil {
		t.Error("a folder with a space was accepted")
	}
}

func TestSystemdUninstallKeepsData(t *testing.T) {
	f := newSdFixture(t)
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	f.cmd.calls = nil
	if err := f.s.run([]string{"uninstall"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.cmd.calls, "|") != "systemctl disable --now hotspotpluslight|systemctl daemon-reload" {
		t.Errorf("commands: %v", f.cmd.calls)
	}
	if _, err := os.Stat(f.s.unitPath); err == nil {
		t.Error("unit file still there")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "hsp.env")); err != nil {
		t.Error("data was removed")
	}
}

func TestSystemdStartStop(t *testing.T) {
	f := newSdFixture(t)
	if err := f.s.run([]string{"start"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.run([]string{"stop"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.cmd.calls, "|") != "systemctl start hotspotpluslight|systemctl stop hotspotpluslight" {
		t.Errorf("commands: %v", f.cmd.calls)
	}
	f.cmd.failOn = "start"
	if err := f.s.run([]string{"start"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error: %v", err)
	}
}

func TestSystemdInstallRefusesSymlinks(t *testing.T) {
	setups := map[string]func(f *sdFixture, other string){
		"dir": func(f *sdFixture, other string) { os.Symlink(other, f.dir) },
		"env": func(f *sdFixture, other string) {
			os.MkdirAll(f.dir, 0o750)
			os.Symlink(filepath.Join(other, "dangling"), filepath.Join(f.dir, "hsp.env"))
		},
		"program": func(f *sdFixture, other string) {
			os.MkdirAll(f.dir, 0o750)
			os.Symlink(other, filepath.Join(f.dir, "hotspotpluslight"))
		},
		"tmp": func(f *sdFixture, other string) {
			os.MkdirAll(f.dir, 0o750)
			os.Symlink(other, filepath.Join(f.dir, "hotspotpluslight.tmp"))
		},
		"data": func(f *sdFixture, other string) {
			os.MkdirAll(filepath.Join(f.dir, "data"), 0o750)
			os.Symlink(other, filepath.Join(f.dir, "data", "hsp.db"))
		},
	}
	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			f := newSdFixture(t)
			other := t.TempDir()
			setup(f, other)
			err := f.s.run([]string{"install", "--dir", f.dir})
			if err == nil || !strings.Contains(err.Error(), "link") && !strings.Contains(err.Error(), "regular") {
				t.Fatalf("error: %v", err)
			}
			if _, err := os.Stat(f.s.unitPath); err == nil {
				t.Error("unit file written")
			}
		})
	}
}

func TestSystemdChownDoesNotFollowLinks(t *testing.T) {
	f := newSdFixture(t)
	f.exists = true
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "secret"), []byte("x"), 0o600)
	os.MkdirAll(f.dir, 0o750)
	os.Symlink(other, filepath.Join(f.dir, "link"))
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.chowns {
		if strings.HasPrefix(c.path, other) {
			t.Errorf("chown followed a link to %s", c.path)
		}
	}
}

func TestSystemdInstallReplacesStaleTmp(t *testing.T) {
	f := newSdFixture(t)
	os.MkdirAll(f.dir, 0o750)
	os.WriteFile(filepath.Join(f.dir, "hotspotpluslight.tmp"), []byte("old"), 0o644)
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.dir, "hotspotpluslight")); string(b) != "program" {
		t.Error("program not copied")
	}
}

func TestSystemdInstallRefusesHiddenFolders(t *testing.T) {
	for _, d := range []string{"/home/me/hsp", "/root/hsp", "/run/user/1000/hsp", "/home"} {
		f := newSdFixture(t)
		err := f.s.run([]string{"install", "--dir", d})
		if err == nil || !strings.Contains(err.Error(), "ProtectHome") {
			t.Errorf("%s: %v", d, err)
		}
	}
}

func TestSystemdInstallChmodsExistingEnv(t *testing.T) {
	f := newSdFixture(t)
	os.MkdirAll(f.dir, 0o750)
	envFile := filepath.Join(f.dir, "hsp.env")
	os.WriteFile(envFile, []byte("HSP_SECRET_KEY=mine\n"), 0o644)
	os.Chmod(envFile, 0o644)
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(envFile); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	if !strings.Contains(f.out.String(), "0600") {
		t.Errorf("output: %s", f.out)
	}
}

func TestSystemdLookupErrorAborts(t *testing.T) {
	f := newSdFixture(t)
	f.s.lookupUser = func(string) (*user.User, error) { return nil, errors.New("ldap down") }
	if err := f.s.run([]string{"install", "--dir", f.dir}); err == nil || !strings.Contains(err.Error(), "ldap down") {
		t.Fatalf("error: %v", err)
	}
	for _, c := range f.cmd.calls {
		if strings.HasPrefix(c, "useradd") {
			t.Error("useradd ran")
		}
	}
}

func TestSystemdUseraddRetriesWithGid(t *testing.T) {
	f := newSdFixture(t)
	f.s.lookupUser = func(string) (*user.User, error) {
		if f.exists {
			return &user.User{Uid: "991", Gid: "992"}, nil
		}
		return nil, user.UnknownUserError("x")
	}
	f.cmd.failOn = "--user-group"
	if err := f.s.run([]string{"install", "--dir", f.dir}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.cmd.calls, "\n"), "--gid hotspotpluslight hotspotpluslight") {
		t.Errorf("commands: %v", f.cmd.calls)
	}
}
