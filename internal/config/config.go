// Package config reads the HSP_* environment (PLAN.md section 9.2).
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/secret"
)

// Config holds the settings read from the HSP_* environment.
type Config struct {
	DataDir            string
	AdminAddr          string
	RadiusAuthAddr     string
	RadiusAcctAddr     string
	SecretKey          []byte // decoded AES-256 key
	AdminUsername      string
	RadiusSharedSecret string // seed for the first start only; empty means generate one
	LogLevel           slog.Level
	LogFormat          string // "text" (key=value) or "json"
	Dev                bool   // HSP_DEV=1: serve the component gallery at /admin/dev/components
}

// DataDir returns the data folder read through getenv. It is the one place that owns the default.
func DataDir(getenv func(string) string) string {
	if v := getenv("HSP_DATA_DIR"); v != "" {
		return v
	}
	return "data"
}

// OpenBrowserOff reports whether HSP_OPEN_BROWSER is 0, false, no or off, which stops the admin page from opening at start.
// It is env-only and never stored.
func OpenBrowserOff(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("HSP_OPEN_BROWSER"))) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}

// EnsureKey returns the base64-encoded AES-256 key to use, trying in order:
// (1) HSP_SECRET_KEY env var, (2) {dataDir}/hsp.key file, (3) auto-generate and
// write to {dataDir}/hsp.key. It creates dataDir if it does not exist.
// It never replaces a key file it cannot read, and it refuses to generate a key
// when hsp.db already exists, because a new key could not decrypt that database.
func EnsureKey(dataDir string, getenv func(string) string) (string, error) {
	if v := getenv("HSP_SECRET_KEY"); v != "" {
		return v, nil
	}
	keyFile := filepath.Join(dataDir, "hsp.key")
	data, err := os.ReadFile(keyFile)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read key file: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "hsp.db")); err == nil {
		return "", fmt.Errorf("%s is missing but hsp.db exists: restore hsp.key from your backup or set HSP_SECRET_KEY", keyFile)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	// O_EXCL makes sure two starting processes never overwrite each other's key.
	f, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return EnsureKey(dataDir, getenv)
	}
	if err != nil {
		return "", fmt.Errorf("write key file: %w", err)
	}
	_, err = f.WriteString(encoded + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(keyFile)
		return "", fmt.Errorf("write key file: %w", err)
	}
	return encoded, nil
}

// LoadWithKey is like Load, but it first finds or creates the secret key with EnsureKey.
func LoadWithKey(getenv func(string) string) (Config, error) {
	rawKey, err := EnsureKey(DataDir(getenv), getenv)
	if err != nil {
		return Config{}, fmt.Errorf("secret key: %w", err)
	}
	return Load(func(k string) string {
		if k == "HSP_SECRET_KEY" {
			return rawKey
		}
		return getenv(k)
	})
}

// Load returns the Config read through getenv. It fails when HSP_SECRET_KEY is
// set but invalid, or when HSP_LOG_LEVEL or HSP_LOG_FORMAT is invalid.
func Load(getenv func(string) string) (Config, error) {
	get := func(name, def string) string {
		if v := getenv("HSP_" + name); v != "" {
			return v
		}
		return def
	}
	c := Config{
		DataDir:            get("DATA_DIR", "data"),
		AdminAddr:          get("ADMIN_ADDR", ":8080"),
		RadiusAuthAddr:     get("RADIUS_AUTH_ADDR", ":1812"),
		RadiusAcctAddr:     get("RADIUS_ACCT_ADDR", ":1813"),
		AdminUsername:      get("ADMIN_USERNAME", "admin"),
		RadiusSharedSecret: get("RADIUS_SHARED_SECRET", ""),
	}
	if v := c.RadiusSharedSecret; v != "" {
		if msg := domain.SecretProblem(v); msg != "" {
			return c, fmt.Errorf("HSP_RADIUS_SHARED_SECRET invalid: %s", msg)
		}
	}
	raw := getenv("HSP_SECRET_KEY")
	if raw == "" {
		return c, fmt.Errorf("HSP_SECRET_KEY is not set; call EnsureKey before Load")
	}
	key, err := secret.ParseKey(raw)
	if err != nil {
		return c, fmt.Errorf("HSP_SECRET_KEY is set but invalid: %w", err)
	}
	c.SecretKey = key
	// Only the four plain names are allowed, so the stored text always matches the settings form.
	lv := strings.ToLower(get("LOG_LEVEL", "info"))
	if lv != "debug" && lv != "info" && lv != "warn" && lv != "error" {
		return c, fmt.Errorf("HSP_LOG_LEVEL invalid: %q (use debug, info, warn or error)", lv)
	}
	if err := c.LogLevel.UnmarshalText([]byte(lv)); err != nil {
		return c, fmt.Errorf("HSP_LOG_LEVEL invalid: %w", err)
	}
	c.Dev = get("DEV", "") == "1"
	c.LogFormat = strings.ToLower(get("LOG_FORMAT", "text"))
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return c, fmt.Errorf("HSP_LOG_FORMAT invalid: %q (use text or json)", c.LogFormat)
	}
	return c, nil
}
