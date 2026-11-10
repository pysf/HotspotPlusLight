package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBackupToCopiesAndReplaces(t *testing.T) {
	s := openTemp(t)
	if _, err := s.db.Exec(`CREATE TABLE t(x); INSERT INTO t VALUES (7)`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "hsp.db.bak-v1.0.0")
	for i := 0; i < 2; i++ { // the second round replaces the first file
		if err := s.BackupTo(context.Background(), dst); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	// The copy holds member passwords: only the owner may read it. (Windows has no such bits.)
	if fi, err := os.Stat(dst); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("backup mode %v, err %v; want 0600", fi.Mode().Perm(), err)
	}
	if _, err := os.Stat(dst + ".tmp"); err == nil {
		t.Error("the .tmp file was left behind")
	}
	db, err := sql.Open("sqlite", "file:"+dst)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT x FROM t`).Scan(&n); err != nil || n != 7 {
		t.Fatalf("value %d, err %v", n, err)
	}
}
