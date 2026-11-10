package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type entry struct{ name, body string }

func makeTarGz(t *testing.T, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func makeZip(t *testing.T, entries ...entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	p := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpackBinary(t *testing.T) {
	good := []entry{{"README.md", "read"}, {"hotspotpluslight", "NEW-LINUX"}}
	goodExe := []entry{{"LICENSE", "lic"}, {"hotspotpluslight.exe", "NEW-WIN"}}
	cases := []struct {
		name    string
		archive string
		asset   string
		want    string // expected file content; empty means an error is expected
	}{
		{"tar good", makeTarGz(t, good...), "x.tar.gz", "NEW-LINUX"},
		{"tar dot slash", makeTarGz(t, entry{"./hotspotpluslight", "NEW-LINUX"}), "x.tar.gz", "NEW-LINUX"},
		{"zip good", makeZip(t, goodExe...), "x.zip", "NEW-WIN"},
		{"tar traversal", makeTarGz(t, entry{"../evil", "x"}, good[1]), "x.tar.gz", ""},
		{"tar traversal in name", makeTarGz(t, entry{"a/../../hotspotpluslight", "x"}), "x.tar.gz", ""},
		{"tar absolute", makeTarGz(t, entry{"/etc/passwd", "x"}, good[1]), "x.tar.gz", ""},
		{"zip traversal", makeZip(t, entry{"../hotspotpluslight.exe", "x"}), "x.zip", ""},
		{"zip backslash", makeZip(t, entry{"..\\hotspotpluslight.exe", "x"}), "x.zip", ""},
		{"tar no binary", makeTarGz(t, entry{"README.md", "x"}), "x.tar.gz", ""},
		{"tar binary in folder", makeTarGz(t, entry{"dir/hotspotpluslight", "x"}), "x.tar.gz", ""},
		{"zip wrong name", makeZip(t, entry{"hotspotpluslight", "x"}), "x.zip", ""},
	}
	for _, c := range cases {
		dest := filepath.Join(t.TempDir(), "exe.new")
		err := unpackBinary(c.archive, dest, c.asset)
		if c.want == "" {
			if err == nil {
				t.Errorf("%s: want an error", c.name)
			}
			if _, serr := os.Stat(dest); serr == nil {
				t.Errorf("%s: dest must not exist after an error", c.name)
			}
			continue
		}
		got, rerr := os.ReadFile(dest)
		if err != nil || rerr != nil || string(got) != c.want {
			t.Errorf("%s: err %v, read %v, content %q", c.name, err, rerr, got)
		}
		if runtime.GOOS != "windows" {
			if fi, _ := os.Stat(dest); fi.Mode().Perm() != 0o755 {
				t.Errorf("%s: mode %v", c.name, fi.Mode())
			}
		}
	}
}

func TestSwap(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "prog")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	write(exe, "OLD")
	write(exe+".new", "NEW")
	write(exe+".old", "OLDER") // from an earlier update; it is replaced
	if err := swap(exe); err != nil {
		t.Fatal(err)
	}
	if read(exe) != "NEW" || read(exe+".old") != "OLD" {
		t.Errorf("exe %q old %q", read(exe), read(exe+".old"))
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Error(".new must be gone")
	}
	// No .new file: the second rename fails and the old file is put back.
	if err := swap(exe); err == nil {
		t.Fatal("want an error")
	}
	if read(exe) != "NEW" {
		t.Errorf("exe must be restored, got %q", read(exe))
	}
}

func TestPlan(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "prog")
	assets := []string{"SHA256SUMS", "hotspotpluslight-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz", "hotspotpluslight-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"}
	if p := Plan(exe, false, assets, true, runtime.GOOS, runtime.GOARCH); !p.OK {
		t.Errorf("want ok, got %q", p.Reason)
	}
	if p := Plan(exe, true, assets, true, runtime.GOOS, runtime.GOARCH); p.OK {
		t.Error("dev build must not be ok")
	}
	if p := Plan(filepath.Join(t.TempDir(), "x.test"), false, assets, true, runtime.GOOS, runtime.GOARCH); p.OK {
		t.Error("test binary must not be ok")
	}
	if p := Plan(exe, false, []string{"SHA256SUMS"}, true, runtime.GOOS, runtime.GOARCH); p.OK {
		t.Error("missing asset must not be ok")
	}
	if p := Plan(filepath.Join(t.TempDir(), "missing", "prog"), false, assets, true, runtime.GOOS, runtime.GOARCH); p.OK {
		t.Error("missing folder must not be ok")
	}
}
