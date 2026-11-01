package domain

import "strconv"

// PlanForm is the raw text of the plan form, so a rejected submission can be
// shown again exactly as typed.
type PlanForm struct {
	Name             string
	DurationDays     string
	SpeedValue       string
	SpeedUnit        string
	BulkValue        string
	BulkUnit         string
	TimeLimitMinutes string
	MaxSessions      string
	AutoResubscribe  bool
}

// NewPlanForm is the form shown for a new plan.
func NewPlanForm() PlanForm {
	return PlanForm{DurationDays: "30", SpeedValue: "0", SpeedUnit: "Mbps", BulkValue: "0", BulkUnit: "GB", TimeLimitMinutes: "0", MaxSessions: "1"}
}

// FormFromPlan renders a stored plan back into form text.
func FormFromPlan(p Plan) PlanForm {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return PlanForm{
		Name: p.Name, DurationDays: strconv.Itoa(p.DurationDays),
		SpeedValue: f(p.SpeedValue), SpeedUnit: p.SpeedUnit, BulkValue: f(p.BulkValue), BulkUnit: p.BulkUnit,
		TimeLimitMinutes: strconv.Itoa(p.TimeLimitMinutes), MaxSessions: strconv.Itoa(p.MaxSessions), AutoResubscribe: p.AutoResubscribe,
	}
}

// ParsePlan validates the form (BR-4, BR-5, DDL checks) and returns the plan.
// ID is left zero. Name uniqueness is the service's concern.
func ParsePlan(f PlanForm) (Plan, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	plan := Plan{
		Name:             p.text("name", f.Name, true, 100),
		DurationDays:     p.integer("duration_days", f.DurationDays, 1, 36500), // BR-5: whole number >= 1
		SpeedValue:       p.decimal("speed_value", f.SpeedValue, 1e9),          // 0 = no rate limit
		SpeedUnit:        p.oneOf("speed_unit", f.SpeedUnit, SpeedUnits),
		BulkValue:        p.decimal("bulk_value", f.BulkValue, 1e12), // 0 = unlimited
		BulkUnit:         p.oneOf("bulk_unit", f.BulkUnit, BulkUnits),
		TimeLimitMinutes: p.integer("time_limit_minutes", f.TimeLimitMinutes, 0, 5_256_000), // 0 = unlimited
		MaxSessions:      p.integer("max_sessions", f.MaxSessions, 1, 1000),
		AutoResubscribe:  f.AutoResubscribe,
	}
	return plan, p.errs
}
