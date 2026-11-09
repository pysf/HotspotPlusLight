package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// queryPlan returns the EXPLAIN QUERY PLAN detail lines of q.
func queryPlan(t *testing.T, s *Store, q string, args ...any) string {
	t.Helper()
	rows, err := s.DB().Query(`EXPLAIN QUERY PLAN `+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, detail)
	}
	return strings.Join(out, " | ")
}

// Review A7: the lookups on the session table must use the expected index, never a scan.
func TestSessionLookupsUseIndexes(t *testing.T) {
	s := seeded(t)
	cases := []struct {
		name, index, query string
		args               []any
	}{
		{"finished-row lookup", "session_key", `SELECT id FROM session WHERE username=? AND acct_session_id=? AND nas_ip=? AND ended_at IS NOT NULL ORDER BY id DESC LIMIT 1`, []any{"u", "s", "ip"}},
		{"member_id lookup", "session_member", `SELECT 1 FROM session x WHERE x.member_id = ?`, []any{1}},
		{"prune chunk", "session_prune", `DELETE FROM session WHERE id IN (SELECT id FROM session WHERE COALESCE(ended_at, last_update_at) < ? LIMIT 1000)`, []any{1}},
	}
	for _, c := range cases {
		plan := queryPlan(t, s, c.query, c.args...)
		t.Logf("%s: %s", c.name, plan)
		if !strings.Contains(plan, "INDEX "+c.index) || strings.Contains(plan, "SCAN session") {
			t.Errorf("%s: want index %s and no table scan, got: %s", c.name, c.index, plan)
		}
	}
}

// Review A7: pruning goes in chunks, removes every old row, and keeps the rows that are still young.
func TestPruneSessionsInChunks(t *testing.T) {
	s := seeded(t)
	ctx := context.Background()
	now := fixed()
	mid := oneMember(t, s)
	total := 2*pruneChunk + 7
	tx, err := s.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < total; i++ {
		if _, err := tx.Exec(`INSERT INTO session(member_id,username,acct_session_id,nas_ip,status,started_at,last_update_at,ended_at) VALUES (?, 'u', ?, 'ip',2,1,1,1)`, mid, i); err != nil {
			t.Fatal(err)
		}
	}
	tx.Exec(`INSERT INTO session(member_id,username,acct_session_id,nas_ip,status,started_at,last_update_at) VALUES (?, 'u','young','ip',3,?,?)`, mid, now.Unix(), now.Unix())
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneSessions(ctx, now.Add(-time.Hour))
	if err != nil || n != total {
		t.Fatalf("pruned %d, %v; want %d", n, err, total)
	}
	var left int
	s.DB().QueryRow(`SELECT count(*) FROM session`).Scan(&left)
	if left != 1 {
		t.Fatalf("%d rows left, want 1", left)
	}
}

// Review A7: a write that arrives during a big prune goes in between two chunks.
func TestPruneLetsOtherWritesIn(t *testing.T) {
	s := seeded(t)
	now := fixed()
	mid := oneMember(t, s)
	const chunk, total = 10, 100
	tx, _ := s.DB().Begin()
	for i := 0; i < total; i++ {
		tx.Exec(`INSERT INTO session(member_id,username,acct_session_id,nas_ip,status,started_at,last_update_at,ended_at) VALUES (?, 'u', ?, 'ip',2,1,1,1)`, mid, i)
	}
	tx.Commit()
	var writeErr error
	var leftAtWrite int
	calls := 0
	n, err := s.pruneSessions(context.Background(), now, chunk, func() {
		calls++
		if calls != 1 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, writeErr = s.DB().ExecContext(ctx, `UPDATE settings SET updated_at=updated_at`)
		s.DB().QueryRow(`SELECT count(*) FROM session`).Scan(&leftAtWrite)
	})
	if err != nil || n != total {
		t.Fatalf("pruned %d, %v; want %d", n, err, total)
	}
	if writeErr != nil {
		t.Errorf("the other write did not get in between chunks: %v", writeErr)
	}
	if leftAtWrite != total-chunk {
		t.Errorf("%d rows left at the write, want %d", leftAtWrite, total-chunk)
	}
}

// oneMember adds a plan and a member and returns the member id.
func oneMember(t *testing.T, s *Store) int64 {
	t.Helper()
	p, err := s.CreatePlan(context.Background(), samplePlan("P"), fixed())
	if err != nil {
		t.Fatal(err)
	}
	return addMember(t, s, "m1", p, nil)
}
