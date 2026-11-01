package domain

import (
	"fmt"
	"strconv"
	"time"
)

// Settings mirrors the single settings row (PLAN.md section 6) minus key_check.
type Settings struct {
	SiteTitle                 string
	Timezone                  string
	RadiusSharedSecret        string
	CoaPort                   int
	AccountingIntervalSeconds int
	DefaultPlanID             *int64 // nil = none
	MacAutoLogin              bool
	AfterLoginURL             string
	BatchUsernamePrefix       string
	BatchCounter              int
	BatchMaxCount             int
	VoucherPasswordLength     int
	VoucherHelp               string
	PublicBaseURL             string
	SessionRetentionDays      int
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
	SiteTitle, Timezone, RadiusSharedSecret, CoaPort, AccountingIntervalSeconds string
	DefaultPlanID                                                               string // "" = none
	MacAutoLogin                                                                bool
	AfterLoginURL, BatchUsernamePrefix, BatchCounter, BatchMaxCount             string
	VoucherPasswordLength, VoucherHelp, PublicBaseURL, SessionRetentionDays     string
}

// FormFromSettings renders stored settings back into form text.
func FormFromSettings(s Settings) SettingsForm {
	def := ""
	if s.DefaultPlanID != nil {
		def = strconv.FormatInt(*s.DefaultPlanID, 10)
	}
	i := strconv.Itoa
	return SettingsForm{
		SiteTitle: s.SiteTitle, Timezone: s.Timezone, RadiusSharedSecret: s.RadiusSharedSecret,
		CoaPort: i(s.CoaPort), AccountingIntervalSeconds: i(s.AccountingIntervalSeconds), DefaultPlanID: def,
		MacAutoLogin: s.MacAutoLogin, AfterLoginURL: s.AfterLoginURL, BatchUsernamePrefix: s.BatchUsernamePrefix,
		BatchCounter: i(s.BatchCounter), BatchMaxCount: i(s.BatchMaxCount), VoucherPasswordLength: i(s.VoucherPasswordLength),
		VoucherHelp: s.VoucherHelp, PublicBaseURL: s.PublicBaseURL, SessionRetentionDays: i(s.SessionRetentionDays),
	}
}

// ParseSettings validates the form with the ranges of the DDL (plus sane
// upper bounds). Existence of the default plan and the timezone name are
// checked by the settings service, which has the data for it.
func ParseSettings(f SettingsForm) (Settings, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	s := Settings{
		SiteTitle:                 p.text("site_title", f.SiteTitle, true, 100),
		Timezone:                  p.text("timezone", f.Timezone, true, 64),
		RadiusSharedSecret:        p.secret("radius_shared_secret", f.RadiusSharedSecret, 128),
		CoaPort:                   p.integer("coa_port", f.CoaPort, 1, 65535),
		AccountingIntervalSeconds: p.integer("accounting_interval_seconds", f.AccountingIntervalSeconds, 10, 86400),
		MacAutoLogin:              f.MacAutoLogin,
		AfterLoginURL:             p.httpURL("after_login_url", f.AfterLoginURL),
		BatchUsernamePrefix:       p.alnum("batch_username_prefix", f.BatchUsernamePrefix, 20),
		BatchCounter:              p.integer("batch_counter", f.BatchCounter, 0, 999_999_999),
		BatchMaxCount:             p.integer("batch_max_count", f.BatchMaxCount, 1, 1000),
		VoucherPasswordLength:     p.integer("voucher_password_length", f.VoucherPasswordLength, 4, 20),
		VoucherHelp:               p.text("voucher_help", f.VoucherHelp, false, 2000),
		PublicBaseURL:             p.httpURL("public_base_url", f.PublicBaseURL),
		SessionRetentionDays:      p.integer("session_retention_days", f.SessionRetentionDays, 0, 36500),
	}
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
