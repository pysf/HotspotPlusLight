// Package config reads the HSP_* environment (PLAN.md section 9.2).
package config

import (
	"fmt"
	"log/slog"
	"strings"

	"hotspotpluslight/internal/secret"
)

type Config struct {
	DataDir            string
	HTTPAddr           string
	AdminAddr          string
	RadiusAuthAddr     string
	RadiusAcctAddr     string
	SecretKey          []byte // decoded AES-256 key
	AdminUsername      string
	RadiusSharedSecret string // first-boot seed only; empty = generate
	LogLevel           slog.Level
}

// Load builds a Config from getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	get := func(name, def string) string {
		if v := getenv("HSP_" + name); v != "" {
			return v
		}
		return def
	}
	c := Config{
		DataDir:            get("DATA_DIR", "data"),
		HTTPAddr:           get("HTTP_ADDR", ":8080"),
		AdminAddr:          get("ADMIN_ADDR", ":8081"),
		RadiusAuthAddr:     get("RADIUS_AUTH_ADDR", ":1812"),
		RadiusAcctAddr:     get("RADIUS_ACCT_ADDR", ":1813"),
		AdminUsername:      get("ADMIN_USERNAME", "admin"),
		RadiusSharedSecret: get("RADIUS_SHARED_SECRET", ""),
	}
	raw := getenv("HSP_SECRET_KEY")
	if raw == "" {
		return c, fmt.Errorf("HSP_SECRET_KEY is required: set it to base64 of 32 random bytes (e.g. `openssl rand -base64 32`) and keep it with your database backups")
	}
	key, err := secret.ParseKey(raw)
	if err != nil {
		return c, fmt.Errorf("HSP_SECRET_KEY invalid: %w", err)
	}
	c.SecretKey = key
	if lv := strings.ToLower(get("LOG_LEVEL", "info")); lv != "" {
		if err := c.LogLevel.UnmarshalText([]byte(lv)); err != nil {
			return c, fmt.Errorf("HSP_LOG_LEVEL invalid: %w", err)
		}
	}
	return c, nil
}
