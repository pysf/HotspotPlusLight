package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDefaultsAndRequired(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := Load(env(map[string]string{"HSP_SECRET_KEY": key}))
	if err != nil {
		t.Fatal(err)
	}
	if c.AdminAddr != ":8080" || c.RadiusAuthAddr != ":1812" ||
		c.RadiusAcctAddr != ":1813" || c.AdminUsername != "admin" || c.DataDir == "" {
		t.Fatalf("defaults: %+v", c)
	}
	if len(c.SecretKey) != 32 {
		t.Fatal("key not decoded")
	}
	if _, err := Load(env(nil)); err == nil || !strings.Contains(err.Error(), "HSP_SECRET_KEY") {
		t.Fatalf("missing key: %v", err)
	}
	if _, err := Load(env(map[string]string{"HSP_SECRET_KEY": "bad"})); err == nil {
		t.Fatal("bad key accepted")
	}
}

func TestDevFlag(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for v, want := range map[string]bool{"": false, "0": false, "1": true} {
		c, err := Load(env(map[string]string{"HSP_SECRET_KEY": key, "HSP_DEV": v}))
		if err != nil || c.Dev != want {
			t.Errorf("HSP_DEV=%q: Dev=%v err=%v", v, c.Dev, err)
		}
	}
}

func TestEnsureKeyGeneratesOnceAndNeverReplaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	first, err := EnsureKey(dir, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	again, err := EnsureKey(dir, env(nil))
	if err != nil || again != first {
		t.Fatalf("second call: %q %v", again, err)
	}
	// hsp.db without hsp.key must not get a new key.
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "hsp.db"), nil, 0o600)
	if _, err := EnsureKey(other, env(nil)); err == nil || !strings.Contains(err.Error(), "hsp.key") {
		t.Fatalf("want hsp.key error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "hsp.key")); err == nil {
		t.Fatal("key file was created next to an existing database")
	}
}

func TestRadiusSharedSecretFromEnvMustBeLettersAndDigits(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := Load(env(map[string]string{"HSP_SECRET_KEY": key, "HSP_RADIUS_SHARED_SECRET": "Good123"})); err != nil {
		t.Fatal(err)
	}
	_, err := Load(env(map[string]string{"HSP_SECRET_KEY": key, "HSP_RADIUS_SHARED_SECRET": "bad secret!"}))
	if err == nil || !strings.Contains(err.Error(), "HSP_RADIUS_SHARED_SECRET") {
		t.Fatalf("want a clear error, got %v", err)
	}
}
