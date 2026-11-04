package store

import (
	"context"
	"testing"
)

func migrated(t *testing.T) *Store {
	s := openTemp(t)
	if _, err := s.Migrate(context.Background(), EmbeddedMigrations(), fixed); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFirstBootSeedsOnce(t *testing.T) {
	s := migrated(t)
	ctx := context.Background()
	in := SeedInput{AdminUsername: "admin", AdminPasswordHash: "h", KeyCheck: "kc", RadiusSecret: "sec", MustChangePassword: true, Now: fixed()}
	first, err := s.FirstBoot(ctx, in)
	if err != nil || !first {
		t.Fatalf("first=%v err=%v", first, err)
	}
	in.AdminPasswordHash = "other"
	first, err = s.FirstBoot(ctx, in)
	if err != nil || first {
		t.Fatalf("second: first=%v err=%v", first, err)
	}
	a, err := s.GetAdmin(ctx)
	if err != nil || a.PasswordHash != "h" || !a.MustChangePassword {
		t.Fatalf("admin %+v %v", a, err)
	}
	plans, _ := s.ListPlans(ctx)
	if len(plans) != 0 {
		t.Fatalf("first boot must create no plan, got %+v", plans)
	}
	kc, ok, _ := s.KeyCheck(ctx)
	if !ok || kc != "kc" {
		t.Fatalf("key_check %q %v", kc, ok)
	}
}
