package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"hotspotpluslight/internal/domain"
)

func seeded(t *testing.T) *Store {
	s := migrated(t)
	if _, err := s.FirstBoot(context.Background(), SeedInput{AdminUsername: "admin", AdminPasswordHash: "h", KeyCheck: "kc", RadiusSecret: "sec", Now: fixed()}); err != nil {
		t.Fatal(err)
	}
	return s
}

func samplePlan(name string) domain.Plan {
	return domain.Plan{Name: name, DurationDays: 30, SpeedValue: 1.5, SpeedUnit: "Mbps", BulkValue: 0.5, BulkUnit: "GB", MaxSessions: 1}
}

func TestPlanCRUD(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	id, err := s.CreatePlan(ctx, samplePlan("Gold"), fixed())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePlan(ctx, samplePlan("gOLD"), fixed()); !errors.Is(err, domain.ErrNameTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	p, err := s.GetPlan(ctx, id)
	if err != nil || p.SpeedValue != 1.5 || p.BulkValue != 0.5 {
		t.Fatalf("%+v %v", p, err)
	}
	p.DurationDays = 7
	if err := s.UpdatePlan(ctx, p, fixed()); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePlan(ctx, domain.Plan{ID: 99, Name: "x", DurationDays: 1, SpeedUnit: "Kbps", BulkUnit: "MB", MaxSessions: 1}, fixed()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("update unknown: %v", err)
	}
	if _, err := s.GetPlan(ctx, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("get unknown: %v", err)
	}
	// A member row makes the RESTRICT foreign key refuse the delete.
	if _, err := s.db.Exec(`INSERT INTO member(username,password_enc,plan_id,subscription_date,created_at,updated_at) VALUES ('a',x'00',?,1,1,1)`, id); err != nil {
		t.Fatal(err)
	}
	var inUse *domain.InUseError
	if err := s.DeletePlan(ctx, id); !errors.As(err, &inUse) || inUse.Members != 1 {
		t.Fatalf("delete in use: %v", err)
	}
	s.db.Exec(`DELETE FROM member`)
	if err := s.DeletePlan(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePlan(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete again: %v", err)
	}
}

func TestSettingsRoundTripAndDefaultPlan(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	id, _ := s.CreatePlan(ctx, samplePlan("Gold"), fixed())
	st, err := s.GetSettings(ctx)
	if err != nil || st.CoaPort != 3799 || st.DefaultPlanID != nil {
		t.Fatalf("%+v %v", st, err)
	}
	st.DefaultPlanID, st.BusinessName = &id, "Cafe"
	if err := s.UpdateSettings(ctx, st, fixed()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings(ctx)
	if got.DefaultPlanID == nil || *got.DefaultPlanID != id || got.BusinessName != "Cafe" {
		t.Fatalf("%+v", got)
	}
	if ok, _ := s.IsDefaultPlan(ctx, id); !ok {
		t.Fatal("IsDefaultPlan")
	}
}

func TestAdminSessions(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	s.InsertAdminSession(ctx, "h1", "c1", 100, 200)
	if csrf, ok, _ := s.TouchAdminSession(ctx, "h1", 150, 250); !ok || csrf != "c1" {
		t.Fatal("live session")
	}
	if _, ok, _ := s.TouchAdminSession(ctx, "h1", 250, 300); ok {
		t.Fatal("expired session touched")
	}
	s.InsertAdminSession(ctx, "h2", "c2", 100, 1000)
	s.PurgeExpiredAdminSessions(ctx, 500)
	if _, ok, _ := s.TouchAdminSession(ctx, "h2", 500, 600); !ok {
		t.Fatal("purge removed a live session")
	}
	if err := s.SetAdminPassword(ctx, "newhash", 600); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.TouchAdminSession(ctx, "h2", 601, 700); ok {
		t.Fatal("sessions survive SetAdminPassword")
	}
	if a, _ := s.GetAdmin(ctx); a.PasswordHash != "newhash" {
		t.Fatal("hash not stored")
	}
	s.InsertAdminSession(ctx, "k1", "c", 100, 1000)
	s.InsertAdminSession(ctx, "k2", "c", 100, 1000)
	if err := s.ChangeAdminPassword(ctx, "h3", 700, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.TouchAdminSession(ctx, "k1", 701, 800); !ok {
		t.Fatal("kept session was deleted")
	}
	if _, ok, _ := s.TouchAdminSession(ctx, "k2", 701, 800); ok {
		t.Fatal("other session survives")
	}
}

// The default plan is never deleted (and never reported as "used by 0 members"),
// even when the caller skipped the service-level check.
func TestDeletePlanRefusesDefault(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	id, _ := s.CreatePlan(ctx, samplePlan("Gold"), fixed())
	if _, err := s.db.Exec(`UPDATE settings SET default_plan_id=? WHERE id=1`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePlan(ctx, id); !errors.Is(err, domain.ErrDefault) {
		t.Fatalf("delete default: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO member(username,password_enc,plan_id,subscription_date,created_at,updated_at) VALUES ('a',x'00',?,1,1,1)`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePlan(ctx, id); !errors.Is(err, domain.ErrDefault) {
		t.Fatalf("delete default with members: %v", err)
	}
	if ok, _ := s.PlanExists(ctx, id); !ok {
		t.Fatal("default plan was deleted")
	}
}

func TestPlanUsageCountsMembersAndBatches(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	a, _ := s.CreatePlan(ctx, samplePlan("A"), fixed())
	b, _ := s.CreatePlan(ctx, samplePlan("B"), fixed())
	for _, u := range []string{"x", "y"} {
		if _, err := s.db.Exec(`INSERT INTO member(username,password_enc,plan_id,subscription_date,created_at,updated_at) VALUES (?,x'00',?,1,1,1)`, u, a); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO batch(label, plan_id, count, duration_days, expires_at, created_at) VALUES ('l',?,1,1,1,1)`, b); err != nil {
		t.Fatal(err)
	}
	use, err := s.PlanUsage(ctx)
	if err != nil || use[a] != (domain.PlanUsage{Members: 2}) || use[b] != (domain.PlanUsage{Batches: 1}) {
		t.Fatalf("%v %v", use, err)
	}
}

func TestListOnlineSessionsHasMemberName(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	id := oneMember(t, s)
	if _, err := s.db.Exec(`UPDATE member SET first_name='Anna', last_name='Keller' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO session(member_id, username, acct_session_id, nas_ip, status, started_at, last_update_at) VALUES (?, 'm1', 's1', '1.1.1.1', 1, 100, 100)`, id); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListOnlineSessions(ctx, time.Unix(50, 0), 10, 0)
	if err != nil || len(rows) != 1 || rows[0].MemberName != "Anna Keller" {
		t.Fatalf("%+v %v", rows, err)
	}
}
