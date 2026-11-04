package config

import (
	"encoding/base64"
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
	if c.AdminAddr != ":8081" || c.RadiusAuthAddr != ":1812" ||
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
