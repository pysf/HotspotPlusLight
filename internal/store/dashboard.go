package store

import (
	"context"
	"database/sql"
	"time"

	"hotspotpluslight/internal/domain"
)

// AddLoginFailure adds 1 to the count of reason on day (BR-27).
func (s *Store) AddLoginFailure(ctx context.Context, day string, reason domain.Reason) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO login_failure_daily(day, reason, count) VALUES (?, ?, 1)
		ON CONFLICT(day, reason) DO UPDATE SET count = count + 1`, day, string(reason))
	return err
}

// LoginFailures returns the failed-login counts of one day, one entry per reason.
func (s *Store) LoginFailures(ctx context.Context, day string) ([]domain.ReasonCount, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT reason, count FROM login_failure_daily WHERE day = ? ORDER BY reason`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ReasonCount
	for rows.Next() {
		var c domain.ReasonCount
		if err := rows.Scan(&c.Reason, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PruneLoginFailures deletes the counts of days before beforeDay and returns how many rows went.
func (s *Store) PruneLoginFailures(ctx context.Context, beforeDay string) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM login_failure_daily WHERE day < ?`, beforeDay)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SessionSpansSince returns the times of every session that was present at or after from: finished ones that
// ended then or later, and open ones updated then or later (BR-25).
func (s *Store) SessionSpansSince(ctx context.Context, from time.Time) ([]domain.SessionSpan, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT started_at, ended_at, last_update_at FROM session WHERE COALESCE(ended_at, last_update_at) >= ?`, from.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SessionSpan
	for rows.Next() {
		var started, last int64
		var ended sql.NullInt64
		if err := rows.Scan(&started, &ended, &last); err != nil {
			return nil, err
		}
		sp := domain.SessionSpan{StartedAt: time.Unix(started, 0), LastUpdate: time.Unix(last, 0)}
		if ended.Valid {
			e := time.Unix(ended.Int64, 0)
			sp.EndedAt = &e
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// OldestSessionStart returns the start of the oldest kept session. ok is false when there are no sessions.
func (s *Store) OldestSessionStart(ctx context.Context) (t time.Time, ok bool, err error) {
	var v sql.NullInt64
	if err := s.r.QueryRowContext(ctx, `SELECT MIN(started_at) FROM session`).Scan(&v); err != nil {
		return time.Time{}, false, err
	}
	if !v.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(v.Int64, 0), true, nil
}

// DailyActive returns, for each day from fromDay to toDay (inclusive), how many members have a usage row (BR-28).
// Days without rows are missing from the result.
func (s *Store) DailyActive(ctx context.Context, fromDay, toDay string) ([]domain.DayCount, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT day, count(*) FROM usage_daily WHERE day >= ? AND day <= ? GROUP BY day ORDER BY day`, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DayCount
	for rows.Next() {
		var c domain.DayCount
		if err := rows.Scan(&c.Day, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ExpiringMembers returns the active members whose expiry is from now to days days later, soonest first (BR-30a),
// at most limit of them, and how many there are in all. The SQL mirrors domain.ExpiresWithin.
func (s *Store) ExpiringMembers(ctx context.Context, now time.Time, days, limit int) ([]domain.AttentionRow, int, error) {
	n := now.Unix()
	last := domain.ExpiryLimit(now, days)
	const where = `m.active = 1 AND m.expires_at IS NOT NULL AND m.expires_at >= ? AND m.expires_at <= ?`
	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM member m WHERE `+where, n, last).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT m.id, m.username, m.expires_at FROM member m WHERE `+where+` ORDER BY m.expires_at, m.id LIMIT ?`, n, last, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.AttentionRow
	for rows.Next() {
		var r domain.AttentionRow
		var exp int64
		if err := rows.Scan(&r.MemberID, &r.Username, &exp); err != nil {
			return nil, 0, err
		}
		r.ExpiresAt = time.Unix(exp, 0)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DataLimitedMembers returns the active members that are not expired and have a plan with a data limit.
// The caller applies the validity window and the 80% rule with domain.QuotaOf, so that logic stays in one place (BR-22).
func (s *Store) DataLimitedMembers(ctx context.Context, now time.Time) ([]domain.MemberRow, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+memberCols+`, `+planColsJoined+` FROM member m JOIN plan p ON p.id = m.plan_id
		WHERE m.active = 1 AND (m.expires_at IS NULL OR m.expires_at >= ?) AND p.bulk_value > 0 ORDER BY m.id`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MemberRow
	for rows.Next() {
		var r domain.MemberRow
		if r.Member, err = scanMember(rows, &r.Plan); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
