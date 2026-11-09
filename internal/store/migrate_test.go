package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func fixed() time.Time { return time.Unix(1_700_000_000, 0) }

func schemaFS(extra ...string) fstest.MapFS {
	m := fstest.MapFS{"0001_init.sql": {Data: []byte(
		"CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL, checksum TEXT NOT NULL);\n" +
			"CREATE TABLE t1 (id INTEGER PRIMARY KEY, v TEXT);")}}
	for i, sqlText := range extra {
		m["000"+string(rune('2'+i))+"_x.sql"] = &fstest.MapFile{Data: []byte(sqlText)}
	}
	return m
}

func versions(t *testing.T, s *Store) []int {
	t.Helper()
	rows, err := s.DB().Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		rows.Scan(&v)
		out = append(out, v)
	}
	return out
}

// E0.09 all migrations apply in order; a second run applies nothing.
func TestMigrateAppliesInOrderAndIsIdempotent(t *testing.T) {
	s := openTemp(t)
	fs := schemaFS("CREATE TABLE t2 (id INTEGER);", "CREATE TABLE t3 (id INTEGER);")
	n, err := s.Migrate(context.Background(), fs, fixed)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if v := versions(t, s); len(v) != 3 || v[0] != 1 || v[2] != 3 {
		t.Fatalf("versions %v", v)
	}
	n, err = s.Migrate(context.Background(), fs, fixed)
	if err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
}

// E0.09 a failing migration rolls back entirely and leaves the previous version.
func TestMigrateFailureRollsBack(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Migrate(context.Background(), schemaFS(), fixed); err != nil {
		t.Fatal(err)
	}
	bad := schemaFS("CREATE TABLE partial (id INTEGER); INSERT INTO nonexistent VALUES (1);")
	if _, err := s.Migrate(context.Background(), bad, fixed); err == nil {
		t.Fatal("expected failure")
	}
	if v := versions(t, s); len(v) != 1 || v[0] != 1 {
		t.Fatalf("versions after failure: %v", v)
	}
	var name string
	err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE name='partial'`).Scan(&name)
	if err != sql.ErrNoRows {
		t.Fatalf("partial table survived: %v %v", name, err)
	}
}

// E0.09 editing an applied migration is detected.
func TestMigrateChecksumMismatch(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Migrate(context.Background(), schemaFS(), fixed); err != nil {
		t.Fatal(err)
	}
	edited := schemaFS()
	edited["0001_init.sql"].Data = append(edited["0001_init.sql"].Data, []byte("\n-- edited")...)
	_, err := s.Migrate(context.Background(), edited, fixed)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
}

// E0.09 upgrading a DB created at an earlier version keeps its data.
func TestMigrateUpgradeKeepsData(t *testing.T) {
	s := openTemp(t)
	full := schemaFS("ALTER TABLE t1 ADD COLUMN extra TEXT NOT NULL DEFAULT 'x';")
	only1 := fstest.MapFS{"0001_init.sql": full["0001_init.sql"]}
	if _, err := s.Migrate(context.Background(), only1, fixed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO t1(v) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Migrate(context.Background(), full, fixed); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	var v, extra string
	if err := s.DB().QueryRow(`SELECT v, extra FROM t1`).Scan(&v, &extra); err != nil || v != "keep" || extra != "x" {
		t.Fatalf("data lost: %v %v %v", v, extra, err)
	}
}

func TestMigrateRejectsGapsAndNewerDB(t *testing.T) {
	s := openTemp(t)
	gap := fstest.MapFS{"0002_x.sql": {Data: []byte("SELECT 1;")}}
	if _, err := s.Migrate(context.Background(), gap, fixed); err == nil {
		t.Fatal("gap accepted")
	}
	if _, err := s.Migrate(context.Background(), schemaFS("CREATE TABLE a(i INT);"), fixed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(context.Background(), schemaFS(), fixed); err == nil {
		t.Fatal("DB newer than binary must be refused")
	}
}

// The embedded 0001_init.sql applies cleanly and creates the section 6 tables.
func TestEmbeddedMigrations(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Migrate(context.Background(), EmbeddedMigrations(), fixed); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"schema_migrations", "admin", "admin_session", "plan", "settings", "batch", "member", "session"} {
		var n string
		if err := s.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil {
			t.Errorf("table %s missing: %v", tbl, err)
		}
	}
}

// TestAppliedChecksumsReturnsRealErrors covers review B6: only a missing
// schema_migrations table means a fresh database.
func TestAppliedChecksumsReturnsRealErrors(t *testing.T) {
	s := openTemp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.appliedChecksums(ctx); err == nil {
		t.Fatal("cancelled context: want an error, got a fresh database")
	}
	if m, err := s.appliedChecksums(context.Background()); err != nil || len(m) != 0 {
		t.Fatalf("fresh database: %v %v", m, err)
	}
}
