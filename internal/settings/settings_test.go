package settings

import (
	"context"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type fakeRepo struct {
	saved  *domain.Settings
	planOK bool
}

func (f *fakeRepo) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{}, nil
}
func (f *fakeRepo) UpdateSettings(_ context.Context, s domain.Settings, _ time.Time) error {
	f.saved = &s
	return nil
}
func (f *fakeRepo) PlanExists(context.Context, int64) (bool, error) { return f.planOK, nil }

func goodForm() domain.SettingsForm {
	return domain.SettingsForm{BusinessName: "T", Timezone: "Asia/Tehran", RadiusSharedSecret: "s", CoaPort: "3799", AccountingIntervalSeconds: "60",
		BatchUsernamePrefix: "wifi", BatchCounter: "1", BatchMaxCount: "30", VoucherPasswordLength: "6", SessionRetentionDays: "90"}
}

func TestUpdate(t *testing.T) {
	r := &fakeRepo{}
	s := New(r, clock.NewFake(time.Unix(0, 0)))
	ctx := context.Background()
	if errs, err := s.Update(ctx, goodForm()); err != nil || len(errs) != 0 || r.saved == nil {
		t.Fatalf("%v %v", errs, err)
	}
	f := goodForm()
	f.Timezone = "Mars/Base"
	if errs, _ := s.Update(ctx, f); errs["timezone"] == "" {
		t.Fatal("bad tz accepted")
	}
	f = goodForm()
	f.DefaultPlanID = "5"
	if errs, _ := s.Update(ctx, f); errs["default_plan_id"] == "" {
		t.Fatal("missing plan accepted")
	}
	r.planOK = true
	if errs, _ := s.Update(ctx, f); len(errs) != 0 {
		t.Fatalf("existing plan rejected: %v", errs)
	}
}

func TestTimezoneValidation(t *testing.T) {
	ctx := context.Background()
	for tz, ok := range map[string]bool{"Europe/Berlin": true, "UTC": true, "Local": false, "": false, "Mars/Base": false} {
		f := goodForm()
		f.Timezone = tz
		errs, err := New(&fakeRepo{}, clock.NewFake(time.Unix(0, 0))).Update(ctx, f)
		if err != nil || (errs["timezone"] == "") != ok {
			t.Errorf("%q: ok=%v errs=%v err=%v", tz, ok, errs, err)
		}
	}
}
