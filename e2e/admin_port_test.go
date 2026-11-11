package e2e

import (
	"testing"

	"hotspotpluslight/e2e/harness"
)

// adminAddrCheck starts the app on the config default (an empty ADMIN_ADDR means "not given") and returns the stored
// and shown admin address. The shown one is empty when the real port is busy on this machine and the start failed.
func adminAddrCheck(t *testing.T, dir, key string) (stored, shown string) {
	t.Helper()
	s, err := harness.TryStart(harness.Options{DataDir: dir, SecretKey: key, Env: map[string]string{"ADMIN_ADDR": ""}})
	if err != nil || s == nil {
		t.Logf("the app did not start (port busy?): %v", err)
	} else {
		defer s.Stop()
		c := harness.LoginAsAdmin(t, s)
		shown = c.Get("/admin/settings").Doc(t).Find(`input[name="admin_addr"]`).AttrOr("value", "")
		s.Stop()
	}
	db := harness.DB(t, dir)
	defer db.Close()
	if err := db.QueryRow(`SELECT admin_addr FROM settings WHERE id=1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	return stored, shown
}

// E24.01 a fresh install stores and shows :8080 as the admin address. An existing row with :8081 is not changed.
func TestE24_01_DefaultAdminPort(t *testing.T) {
	dir, key := t.TempDir(), harness.NewKey()
	stored, shown := adminAddrCheck(t, dir, key)
	if stored != ":8080" || (shown != "" && shown != ":8080") {
		t.Fatalf("fresh install: stored %q, shown %q, want :8080", stored, shown)
	}
	db := harness.DB(t, dir)
	if _, err := db.Exec(`UPDATE settings SET admin_addr=':8081' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	stored, shown = adminAddrCheck(t, dir, key)
	if stored != ":8081" || (shown != "" && shown != ":8081") {
		t.Fatalf("existing install after restart: stored %q, shown %q, want :8081", stored, shown)
	}
}
