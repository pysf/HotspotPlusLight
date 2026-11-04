package store

import "testing"

// E11.30 the DSN helper builds a safe SQLite URI for Windows and Unix paths on every OS.
func TestE11_30_DSN(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\ProgramData\HotspotPlusLight\data\hsp.db`, "file:///C:/ProgramData/HotspotPlusLight/data/hsp.db"},
		{`C:/Program Files/x/hsp.db`, "file:///C:/Program%20Files/x/hsp.db"},
		{`d:\my#data\what?\100%\hsp.db`, "file:///d:/my%23data/what%3F/100%25/hsp.db"},
		{`\\server\share\hsp.db`, "file:////server/share/hsp.db"},
		{`/var/lib/hsp/hsp.db`, "file:///var/lib/hsp/hsp.db"},
		{`/tmp/a b/c#d/hsp.db`, "file:///tmp/a%20b/c%23d/hsp.db"},
		{`data/hsp.db`, "file:data/hsp.db"},
		{`data\sub/hsp.db`, "file:data%5Csub/hsp.db"}, // not a Windows path: the backslash is a real name character
	}
	for _, c := range cases {
		want := c.want + "?" + dsnParams
		if got := dsn(c.in, false); got != want {
			t.Errorf("DSN(%q)\n got  %s\n want %s", c.in, got, want)
		}
	}
	// On Windows a backslash separates folders in relative paths too.
	want := "file:data/sub/hsp.db?" + dsnParams
	if got := dsn(`data\sub\hsp.db`, true); got != want {
		t.Errorf("windows relative: got %s want %s", got, want)
	}
}

// The read pool refuses writes; the write pool accepts them.
func TestReadPoolIsQueryOnly(t *testing.T) {
	s := migrated(t)
	if _, err := s.r.Exec(`CREATE TABLE ro_probe(x)`); err == nil {
		t.Fatal("write on the read pool must fail")
	}
	if _, err := s.db.Exec(`CREATE TABLE ro_probe(x)`); err != nil {
		t.Fatalf("write pool: %v", err)
	}
	if _, err := s.r.Exec(`INSERT INTO ro_probe(x) VALUES (1)`); err == nil {
		t.Fatal("insert on the read pool must fail")
	}
}
