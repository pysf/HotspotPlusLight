package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"hotspotpluslight/internal/domain"
)

func addMember(t *testing.T, s *Store, name string, plan int64, exp *time.Time) int64 {
	t.Helper()
	id, err := s.CreateMember(context.Background(), domain.MemberInput{Username: name, Active: true, PlanID: plan, ExpiresAt: exp, FirstName: "Ann", LastName: "Lee"}, []byte("x"), fixed())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMemberCRUDAndUniqueness(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), fixed())
	id := addMember(t, s, "Alice", p, nil)
	if _, err := s.CreateMember(ctx, domain.MemberInput{Username: "aLICE", PlanID: p}, []byte("x"), fixed()); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("case-insensitive duplicate: %v", err)
	}
	m, err := s.GetMember(ctx, id)
	if err != nil || m.Username != "Alice" || !m.SubscriptionDate.Equal(fixed().UTC()) || m.ExpiresAt != nil {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := s.GetMember(ctx, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if err := s.UpdateMember(ctx, id, domain.MemberInput{Username: "ALICE", PlanID: p, Active: false}, fixed()); err != nil {
		t.Fatal(err)
	}
	// Update never touches the window or counters (BR-13a).
	s.db.Exec(`UPDATE member SET used_download_bytes=5, extra_bulk_bytes=7 WHERE id=?`, id)
	s.UpdateMember(ctx, id, domain.MemberInput{Username: "ALICE", PlanID: p, Active: true}, fixed().Add(time.Hour))
	if m, _ = s.GetMember(ctx, id); m.UsedDownload != 5 || m.ExtraBulkBytes != 7 || !m.SubscriptionDate.Equal(fixed().UTC()) {
		t.Fatalf("update changed counters: %+v", m)
	}
	if err := s.RenewMember(ctx, id, "manual", fixed().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if m, _ = s.GetMember(ctx, id); m.UsedDownload != 0 || m.ExtraBulkBytes != 0 || m.RenewalCount != 1 || m.LastRenewalKind != "manual" || m.LastRenewedAt == nil {
		t.Fatalf("renew: %+v", m)
	}
}

func TestFilterSelectionAndDelete(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	now := fixed()
	p1, _ := s.CreatePlan(ctx, samplePlan("P1"), now)
	p2, _ := s.CreatePlan(ctx, samplePlan("P2"), now)
	past, edge := now.Add(-time.Second), now
	a := addMember(t, s, "expired1", p1, &past)
	addMember(t, s, "edge", p1, &edge)
	b := addMember(t, s, "expired2", p2, &past)
	addMember(t, s, "plain", p1, nil)
	s.db.Exec(`INSERT INTO session(member_id,username,acct_session_id,nas_ip,status,started_at,last_update_at) VALUES (?, 'expired1','s1','10.0.0.1',3,?,?)`, a, now.Unix(), now.Unix())

	count := func(f domain.MemberFilter) int {
		n, err := s.CountFiltered(ctx, f, now)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(domain.MemberFilter{Status: domain.StatusExpired}); n != 2 {
		t.Fatalf("expired: %d", n)
	}
	// BR-24: "edge" expires exactly now, so it is expiring (not active).
	if n := count(domain.MemberFilter{Status: domain.StatusActive}); n != 1 { // plain
		t.Fatalf("active: %d", n)
	}
	if n := count(domain.MemberFilter{Status: domain.StatusExpiring}); n != 1 { // edge
		t.Fatalf("expiring: %d", n)
	}
	if n := count(domain.MemberFilter{Status: domain.StatusExpired, PlanID: p2}); n != 1 {
		t.Fatalf("expired+plan: %d", n)
	}
	if n := count(domain.MemberFilter{Query: "ann lee"}); n != 4 {
		t.Fatalf("full-name search: %d", n)
	}
	if n := count(domain.MemberFilter{Query: "%"}); n != 0 {
		t.Fatalf("literal percent: %d", n)
	}
	members, online, err := s.SelectionStats(ctx, domain.MemberSelection{IDs: []int64{a, b}}, now, now.Add(-time.Hour))
	if err != nil || members != 2 || online != 1 {
		t.Fatalf("stats %d %d %v", members, online, err)
	}
	targets, err := s.OpenSessions(ctx, domain.MemberSelection{IDs: []int64{a, b}}, now)
	if err != nil || len(targets) != 1 || targets[0].NASIP != "10.0.0.1" || targets[0].AcctSessionID != "s1" {
		t.Fatalf("targets %+v %v", targets, err)
	}
	if n, _, _ := s.DeleteMembers(ctx, domain.MemberSelection{}, now); n != 0 {
		t.Fatal("an empty selection must delete nothing")
	}
	n, open, err := s.DeleteMembers(ctx, domain.MemberSelection{Filter: &domain.MemberFilter{Status: domain.StatusExpired}}, now)
	if err != nil || n != 2 || len(open) > 1 {
		t.Fatalf("delete by filter: %d %v", n, err)
	}
	var sessions int
	s.db.QueryRow(`SELECT count(*) FROM session`).Scan(&sessions)
	if sessions != 0 || count(domain.MemberFilter{}) != 2 {
		t.Fatalf("sessions %d, members %d", sessions, count(domain.MemberFilter{}))
	}
}

func TestListMembersPagingAndJoin(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), fixed())
	for _, n := range []string{"a", "b", "c"} {
		addMember(t, s, n, p, nil)
	}
	rows, err := s.ListMembers(ctx, domain.MemberFilter{}, 2, 0, fixed(), fixed())
	if err != nil || len(rows) != 2 || rows[0].Member.Username != "c" || rows[0].Plan.Name != "P" {
		t.Fatalf("%+v %v", rows, err)
	}
	if rows, _ = s.ListMembers(ctx, domain.MemberFilter{}, 2, 2, fixed(), fixed()); len(rows) != 1 || rows[0].Member.Username != "a" {
		t.Fatalf("page 2: %+v", rows)
	}
}

// The SQL status filter and the Go rule give the same answer at every boundary (BR-24).
func TestStatusClauseMatchesGoRule(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	now := fixed()
	p, _ := s.CreatePlan(ctx, samplePlan("P"), now)
	soon := time.Duration(domain.ExpiringDays) * 24 * time.Hour
	offsets := []time.Duration{-time.Second, 0, time.Second, soon - time.Second, soon, soon + time.Second, 30 * 24 * time.Hour}
	type want struct{ name, status string }
	var all []want
	for i, active := range []bool{true, false} {
		for j, off := range append([]time.Duration{-1}, offsets...) {
			name := "m" + string(rune('a'+i)) + string(rune('a'+j))
			var exp *time.Time
			if off != -1 {
				e := now.Add(off)
				exp = &e
			}
			id, err := s.CreateMember(ctx, domain.MemberInput{Username: name, Active: active, PlanID: p, ExpiresAt: exp}, []byte("x"), now)
			if err != nil {
				t.Fatal(err)
			}
			_ = id
			all = append(all, want{name, domain.MemberStatus(active, exp, now)})
		}
	}
	counts := map[string]int{}
	for _, st := range []string{domain.StatusActive, domain.StatusExpiring, domain.StatusExpired, domain.StatusInactive} {
		rows, err := s.ListMembers(ctx, domain.MemberFilter{Status: st}, 100, 0, now, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			counts[st]++
			for _, w := range all {
				if w.name == r.Member.Username && w.status != st {
					t.Errorf("%s listed under %s, Go rule says %s", w.name, st, w.status)
				}
			}
		}
	}
	exp := map[string]int{}
	for _, w := range all {
		exp[w.status]++
	}
	for st, n := range exp {
		if counts[st] != n {
			t.Errorf("%s: SQL %d, Go %d", st, counts[st], n)
		}
	}
	c, err := s.CountStatuses(ctx, domain.MemberFilter{Status: domain.StatusExpired}, now) // Status is ignored
	if err != nil {
		t.Fatal(err)
	}
	if c.All != len(all) || c.Active != exp["active"] || c.Expiring != exp["expiring"] || c.Expired != exp["expired"] || c.Inactive != exp["inactive"] {
		t.Errorf("counts %+v, want %v of %d", c, exp, len(all))
	}
	if c.Active+c.Expiring+c.Expired+c.Inactive != c.All {
		t.Errorf("statuses do not add up: %+v", c)
	}
	if n, _ := s.CountStatuses(ctx, domain.MemberFilter{Query: "zzz"}, now); n.All != 0 {
		t.Errorf("query not applied to the counts: %+v", n)
	}
}
