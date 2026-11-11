package domain

import (
	"strings"
	"testing"
)

func okForm() PlanForm { f := NewPlanForm(); f.Name = "Gold"; return f }

func TestParsePlanValid(t *testing.T) {
	f := okForm()
	f.SpeedValue, f.BulkValue, f.BulkUnit = "1.5", "0.5", "GB"
	p, errs := ParsePlan(f)
	if len(errs) != 0 || p.SpeedValue != 1.5 || p.BulkValue != 0.5 || p.DurationDays != 30 || p.MaxSessions != 1 {
		t.Fatalf("%+v %v", p, errs)
	}
	if got := FormFromPlan(p); got.SpeedValue != "1.5" || got.BulkValue != "0.5" || got.Name != "Gold" {
		t.Fatalf("round trip %+v", got)
	}
}

func TestParsePlanInvalid(t *testing.T) {
	for field, mutate := range map[string]func(*PlanForm){
		"name":               func(f *PlanForm) { f.Name = "  " },
		"duration_days":      func(f *PlanForm) { f.DurationDays = "0" },
		"max_sessions":       func(f *PlanForm) { f.MaxSessions = "1.5" },
		"speed_value":        func(f *PlanForm) { f.SpeedValue = "-1" },
		"speed_unit":         func(f *PlanForm) { f.SpeedUnit = "bps" },
		"bulk_value":         func(f *PlanForm) { f.BulkValue = "NaN" },
		"bulk_unit":          func(f *PlanForm) { f.BulkUnit = "TB" },
		"time_limit_minutes": func(f *PlanForm) { f.TimeLimitMinutes = "-1" },
	} {
		f := okForm()
		mutate(&f)
		if _, errs := ParsePlan(f); errs[field] == "" {
			t.Errorf("%s not rejected: %v", field, errs)
		}
	}
}

func goodSettingsForm() SettingsForm {
	return SettingsForm{
		BusinessName: "T", Timezone: "UTC", RadiusSharedSecret: "s", CoaPort: "3799", AccountingIntervalSeconds: "60",
		BatchUsernamePrefix: "wifi", BatchCounter: "1360", BatchMaxCount: "30", VoucherPasswordLength: "6", SessionRetentionDays: "90",
		AdminAddr: ":8080", RadiusAuthAddr: ":1812", RadiusAcctAddr: ":1813", LogLevel: "info", LogFormat: "text",
	}
}

func TestParseSettings(t *testing.T) {
	good := goodSettingsForm()
	if s, errs := ParseSettings(good); len(errs) != 0 || s.DefaultPlanID != nil {
		t.Fatalf("%+v %v", s, errs)
	}
	good.DefaultPlanID = "4"
	if s, _ := ParseSettings(good); s.DefaultPlanID == nil || *s.DefaultPlanID != 4 {
		t.Fatal("default plan not parsed")
	}
	for field, mutate := range map[string]func(*SettingsForm){
		"coa_port":                    func(f *SettingsForm) { f.CoaPort = "0" },
		"accounting_interval_seconds": func(f *SettingsForm) { f.AccountingIntervalSeconds = "9" },
		"batch_username_prefix":       func(f *SettingsForm) { f.BatchUsernamePrefix = "a-b" },
		"default_plan_id":             func(f *SettingsForm) { f.DefaultPlanID = "x" },
		"session_retention_days":      func(f *SettingsForm) { f.SessionRetentionDays = "-1" },
		"business_name":               func(f *SettingsForm) { f.BusinessName = "Hotel\nAria" },
		"admin_addr":                  func(f *SettingsForm) { f.AdminAddr = "8080" }, // missing colon
		"radius_auth_addr":            func(f *SettingsForm) { f.RadiusAuthAddr = "" },
		"radius_acct_addr":            func(f *SettingsForm) { f.RadiusAcctAddr = ":65536" }, // port out of range
		"log_level":                   func(f *SettingsForm) { f.LogLevel = "verbose" },
		"log_format":                  func(f *SettingsForm) { f.LogFormat = "yaml" },
	} {
		f := goodSettingsForm()
		mutate(&f)
		if _, errs := ParseSettings(f); errs[field] == "" {
			t.Errorf("%s not rejected: %v", field, errs)
		}
	}
}

// The owner replaced the old "kept verbatim" rule on 2026-11-07: a secret is letters and digits only.
func TestSharedSecretLettersAndDigitsOnly(t *testing.T) {
	f := goodSettingsForm()
	f.BatchCounter = "1"
	f.RadiusSharedSecret = "Abc123xyz"
	s, errs := ParseSettings(f)
	if len(errs) != 0 || s.RadiusSharedSecret != "Abc123xyz" {
		t.Fatalf("%q %v", s.RadiusSharedSecret, errs)
	}
	if got := FormFromSettings(s).RadiusSharedSecret; got != "Abc123xyz" {
		t.Fatalf("round trip: %q", got)
	}
	for _, bad := range []string{"", "   ", "\t", " se cret ", "abc def", "abc-def", "p@ss", "caf\u00e9", "abc\n", strings.Repeat("a", SecretMaxLen+1)} {
		f.RadiusSharedSecret = bad
		if _, errs := ParseSettings(f); errs["radius_shared_secret"] == "" {
			t.Errorf("%q accepted", bad)
		}
	}
	f.RadiusSharedSecret = strings.Repeat("a", SecretMaxLen)
	if _, errs := ParseSettings(f); errs["radius_shared_secret"] != "" {
		t.Errorf("max length rejected: %v", errs)
	}
}

// An older database may hold a secret with symbols. Saving it unchanged must still work.
func TestStoredSecretWithSymbolsIsKept(t *testing.T) {
	f := goodSettingsForm()
	f.RadiusSharedSecret = "old s3cret!"
	if _, errs := ParseSettingsKeeping(f, "old s3cret!"); errs["radius_shared_secret"] != "" {
		t.Errorf("unchanged stored secret rejected: %v", errs)
	}
	if _, errs := ParseSettingsKeeping(f, "different"); errs["radius_shared_secret"] == "" {
		t.Error("a new secret with symbols accepted")
	}
	f.RadiusSharedSecret = "bad\nsecret"
	if _, errs := ParseSettingsKeeping(f, "bad\nsecret"); errs["radius_shared_secret"] == "" {
		t.Error("control characters accepted for a stored secret")
	}
}

func TestTimezoneNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range TimezoneNames {
		if seen[n] {
			t.Errorf("%s is listed twice", n)
		}
		seen[n] = true
	}
	for _, want := range []string{"UTC", "Asia/Tehran", "Europe/London"} {
		if !seen[want] {
			t.Errorf("%s is missing", want)
		}
	}
}

func TestRestartNeeded(t *testing.T) {
	run := ServerSettings{":8080", ":1812", ":1813", "info", "text"}
	if RestartNeeded(run, run) || RestartNeeded(ServerSettings{":8080", ":1812", ":1813", "INFO", "text"}, run) {
		t.Fatal("same settings must not need a restart")
	}
	for name, mutate := range map[string]func(*ServerSettings){
		"admin":  func(s *ServerSettings) { s.AdminAddr = ":9090" },
		"auth":   func(s *ServerSettings) { s.RadiusAuthAddr = "10.0.0.1:1812" },
		"acct":   func(s *ServerSettings) { s.RadiusAcctAddr = ":2000" },
		"level":  func(s *ServerSettings) { s.LogLevel = "debug" },
		"format": func(s *ServerSettings) { s.LogFormat = "json" },
	} {
		saved := run
		mutate(&saved)
		if !RestartNeeded(saved, run) {
			t.Errorf("%s change not detected", name)
		}
	}
}
