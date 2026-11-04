package domain

import "testing"

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

func TestParseSettings(t *testing.T) {
	good := SettingsForm{SiteTitle: "T", Timezone: "UTC", RadiusSharedSecret: "s", CoaPort: "3799", AccountingIntervalSeconds: "60",
		BatchUsernamePrefix: "wifi", BatchCounter: "1360", BatchMaxCount: "30", VoucherPasswordLength: "6", SessionRetentionDays: "90"}
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
	} {
		f := good
		mutate(&f)
		if _, errs := ParseSettings(f); errs[field] == "" {
			t.Errorf("%s not rejected: %v", field, errs)
		}
	}
}

func TestSharedSecretKeptVerbatim(t *testing.T) {
	f := SettingsForm{SiteTitle: "T", Timezone: "UTC", RadiusSharedSecret: "  se cret  ", CoaPort: "3799", AccountingIntervalSeconds: "60",
		BatchUsernamePrefix: "wifi", BatchCounter: "1", BatchMaxCount: "30", VoucherPasswordLength: "6", SessionRetentionDays: "90"}
	s, errs := ParseSettings(f)
	if len(errs) != 0 || s.RadiusSharedSecret != "  se cret  " {
		t.Fatalf("%q %v", s.RadiusSharedSecret, errs)
	}
	if got := FormFromSettings(s).RadiusSharedSecret; got != "  se cret  " {
		t.Fatalf("round trip: %q", got)
	}
	for _, bad := range []string{"", "   ", "\t"} {
		f.RadiusSharedSecret = bad
		if _, errs := ParseSettings(f); errs["radius_shared_secret"] == "" {
			t.Errorf("%q accepted", bad)
		}
	}
}
