package domain

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Settings mirrors the single settings row (PLAN.md section 6) minus key_check.
type Settings struct {
	BusinessName              string
	Timezone                  string
	RadiusSharedSecret        string
	CoaPort                   int
	AccountingIntervalSeconds int
	DefaultPlanID             *int64 // nil = none
	BatchUsernamePrefix       string
	BatchCounter              int
	BatchMaxCount             int
	VoucherPasswordLength     int
	VoucherHelp               string
	SessionRetentionDays      int
	// The server settings take effect after a restart.
	AdminAddr      string
	RadiusAuthAddr string
	RadiusAcctAddr string
	LogLevel       string
	LogFormat      string
}

// Location is the display zone. A bad zone name is a corrupt-settings error (the settings form validates it).
func (s Settings) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("settings.timezone %q: %w", s.Timezone, err)
	}
	return loc, nil
}

// SettingsForm is the raw text of the settings form.
type SettingsForm struct {
	BusinessName, Timezone, RadiusSharedSecret, CoaPort, AccountingIntervalSeconds string
	DefaultPlanID                                                                  string // "" = none
	BatchUsernamePrefix, BatchCounter, BatchMaxCount                               string
	VoucherPasswordLength, VoucherHelp, SessionRetentionDays                       string
	// The server settings take effect after a restart.
	AdminAddr, RadiusAuthAddr, RadiusAcctAddr, LogLevel, LogFormat string
}

// FormFromSettings renders stored settings back into form text.
func FormFromSettings(s Settings) SettingsForm {
	def := ""
	if s.DefaultPlanID != nil {
		def = strconv.FormatInt(*s.DefaultPlanID, 10)
	}
	i := strconv.Itoa
	return SettingsForm{
		BusinessName: s.BusinessName, Timezone: s.Timezone, RadiusSharedSecret: s.RadiusSharedSecret,
		CoaPort: i(s.CoaPort), AccountingIntervalSeconds: i(s.AccountingIntervalSeconds), DefaultPlanID: def,
		BatchUsernamePrefix: s.BatchUsernamePrefix,
		BatchCounter:        i(s.BatchCounter), BatchMaxCount: i(s.BatchMaxCount), VoucherPasswordLength: i(s.VoucherPasswordLength),
		VoucherHelp: s.VoucherHelp, SessionRetentionDays: i(s.SessionRetentionDays),
		AdminAddr: s.AdminAddr, RadiusAuthAddr: s.RadiusAuthAddr, RadiusAcctAddr: s.RadiusAcctAddr,
		LogLevel: s.LogLevel, LogFormat: s.LogFormat,
	}
}

// ParseSettings validates the form with the ranges of the DDL (plus sane
// upper bounds). Existence of the default plan and the timezone name are
// checked by the settings service, which has the data for it.
func ParseSettings(f SettingsForm) (Settings, FieldErrors) { return ParseSettingsKeeping(f, "") }

// ParseSettingsKeeping is ParseSettings for a row that already holds storedSecret.
// Submitting that same secret again is accepted even if it breaks the current secret rule.
func ParseSettingsKeeping(f SettingsForm, storedSecret string) (Settings, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	s := Settings{
		BusinessName:              p.line("business_name", f.BusinessName, false, 100),
		Timezone:                  p.text("timezone", f.Timezone, true, 64),
		RadiusSharedSecret:        p.secret("radius_shared_secret", f.RadiusSharedSecret, storedSecret),
		CoaPort:                   p.integer("coa_port", f.CoaPort, 1, 65535),
		AccountingIntervalSeconds: p.integer("accounting_interval_seconds", f.AccountingIntervalSeconds, 10, 86400),
		BatchUsernamePrefix:       p.alnum("batch_username_prefix", f.BatchUsernamePrefix, 20),
		BatchCounter:              p.integer("batch_counter", f.BatchCounter, 0, 999_999_999),
		BatchMaxCount:             p.integer("batch_max_count", f.BatchMaxCount, 1, 1000),
		VoucherPasswordLength:     p.integer("voucher_password_length", f.VoucherPasswordLength, 4, 20),
		VoucherHelp:               p.text("voucher_help", f.VoucherHelp, false, 2000),
		SessionRetentionDays:      p.integer("session_retention_days", f.SessionRetentionDays, 0, 36500),
		AdminAddr:                 p.netAddr("admin_addr", f.AdminAddr),
		RadiusAuthAddr:            p.netAddr("radius_auth_addr", f.RadiusAuthAddr),
		RadiusAcctAddr:            p.netAddr("radius_acct_addr", f.RadiusAcctAddr),
		LogLevel:                  p.logLevel("log_level", f.LogLevel),
		LogFormat:                 p.logFormat("log_format", f.LogFormat),
	}
	checkDistinctPorts(p.errs, s)
	if f.DefaultPlanID != "" {
		id, err := strconv.ParseInt(f.DefaultPlanID, 10, 64)
		if err != nil || id < 1 {
			p.errs.add("default_plan_id", "Choose a plan from the list.")
		} else {
			s.DefaultPlanID = &id
		}
	}
	return s, p.errs
}

// netAddr validates a host:port address string and returns it, or records an error.
// Port 0 is allowed (the OS picks a free port at bind time).
func (p *parser) netAddr(field, v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		p.errs.add(field, "Required.")
		return v
	}
	_, portStr, err := net.SplitHostPort(v)
	if err != nil {
		p.errs.add(field, "Must be host:port or :port (e.g. :8081).")
		return v
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		p.errs.add(field, "Port must be between 0 and 65535.")
		return v
	}
	return v
}

// logLevel validates a log level string and returns its canonical lowercase form, or records an error.
func (p *parser) logLevel(field, v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "debug", "info", "warn", "error":
		return v
	}
	p.errs.add(field, "Must be debug, info, warn, or error.")
	return v
}

// logFormat validates a log format string and returns it, or records an error.
func (p *parser) logFormat(field, v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "text", "json":
		return v
	}
	p.errs.add(field, "Must be text or json.")
	return v
}

// checkDistinctPorts rejects two server addresses that use the same non-zero port, because the second bind would fail at restart.
func checkDistinctPorts(errs FieldErrors, s Settings) {
	type entry struct{ field, addr string }
	seen := map[string]string{}
	for _, e := range []entry{{"admin_addr", s.AdminAddr}, {"radius_auth_addr", s.RadiusAuthAddr}, {"radius_acct_addr", s.RadiusAcctAddr}} {
		_, port, err := net.SplitHostPort(e.addr)
		if err != nil || port == "0" {
			continue
		}
		if other, dup := seen[port]; dup {
			errs.add(e.field, "Port "+port+" is already used by "+other+".")
			continue
		}
		seen[port] = e.field
	}
}

// ServerSettings are the settings that only take effect after a restart.
type ServerSettings struct {
	AdminAddr, RadiusAuthAddr, RadiusAcctAddr, LogLevel, LogFormat string
}

// Server returns the server settings part of s.
func (s Settings) Server() ServerSettings {
	return ServerSettings{s.AdminAddr, s.RadiusAuthAddr, s.RadiusAcctAddr, s.LogLevel, s.LogFormat}
}

// RestartNeeded reports whether the saved server settings differ from the ones the running server uses.
// Addresses are compared as exact text; the log level ignores case.
func RestartNeeded(saved, running ServerSettings) bool {
	return saved.AdminAddr != running.AdminAddr || saved.RadiusAuthAddr != running.RadiusAuthAddr ||
		saved.RadiusAcctAddr != running.RadiusAcctAddr || saved.LogFormat != running.LogFormat ||
		!strings.EqualFold(saved.LogLevel, running.LogLevel)
}
