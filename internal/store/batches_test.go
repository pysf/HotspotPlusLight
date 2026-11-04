package store

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hotspotpluslight/internal/domain"
)

func newBatch(planID int64, n int) domain.NewBatch {
	enc := make([][]byte, n)
	for i := range enc {
		enc[i] = []byte("x")
	}
	return domain.NewBatch{Input: domain.BatchInput{PlanID: planID, Count: n}, DurationDays: 30, ExpiresAt: fixed().AddDate(0, 0, 31), PasswordsEnc: enc}
}

func counter(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT batch_counter FROM settings WHERE id=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A username that is already taken is skipped and the counter moves past it (review A2, BR-15).
func TestCreateBatchSkipsTakenUsername(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), fixed())
	start := counter(t, s)
	addMember(t, s, "wifi"+itoa(start+2), p, nil) // the 3rd voucher name is taken
	_, names, err := s.CreateBatch(ctx, newBatch(p, 5), fixed())
	if err != nil || len(names) != 5 || names[2] != "wifi"+itoa(start+3) || counter(t, s) != start+6 {
		t.Fatalf("%v %v counter %d", names, err, counter(t, s))
	}
	var members int
	s.db.QueryRow(`SELECT count(*) FROM member`).Scan(&members)
	if members != 6 {
		t.Fatalf("%d members, want 6", members)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// A plan deleted after the service checked it surfaces as ErrPlanGone, not a raw FK error.
func TestCreateBatchPlanGone(t *testing.T) {
	s := seeded(t)
	start := counter(t, s)
	if _, _, err := s.CreateBatch(context.Background(), newBatch(9999, 2), fixed()); !errors.Is(err, domain.ErrPlanGone) {
		t.Fatalf("%v", err)
	}
	if counter(t, s) != start {
		t.Fatal("counter moved")
	}
}

// The batch selection of DeleteMembers returns the open sessions it saw inside its
// transaction (so the caller can kick late arrivals) and drops the batch row too.
func TestDeleteBatchMembersReturnsOpenSessions(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), fixed())
	b, names, err := s.CreateBatch(ctx, newBatch(p, 2), fixed())
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`INSERT INTO session(member_id,username,acct_session_id,nas_ip,status,started_at,last_update_at)
		SELECT id, username, 'late1', '10.0.0.1', 1, 1, 1 FROM member WHERE username=?`, names[1])
	n, open, err := s.DeleteMembers(ctx, domain.MemberSelection{BatchID: &b.ID}, fixed())
	if err != nil || n != 2 || len(open) != 1 || open[0].AcctSessionID != "late1" {
		t.Fatalf("%d %+v %v", n, open, err)
	}
	if _, err := s.GetBatch(ctx, b.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("batch row survived: %v", err)
	}
}

// A plan held by members and an (also empty) batch reports both counts.
func TestDeletePlanInUseCounts(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), fixed())
	addMember(t, s, "solo", p, nil)
	if _, _, err := s.CreateBatch(ctx, newBatch(p, 2), fixed()); err != nil {
		t.Fatal(err)
	}
	var in *domain.InUseError
	if err := s.DeletePlan(ctx, p); !errors.As(err, &in) || in.Members != 3 || in.Batches != 1 {
		t.Fatalf("%v", err)
	}
}
