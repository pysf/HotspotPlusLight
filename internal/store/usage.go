package store

import (
	"context"

	"hotspotpluslight/internal/domain"
)

// DailyTotals sums all members per day for the inclusive day range; days without rows are absent.
func (s *Store) DailyTotals(ctx context.Context, fromDay, toDay string) ([]domain.UsageDay, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT day, SUM(down), SUM(up), SUM(seconds) FROM usage_daily
		WHERE day >= ? AND day <= ? GROUP BY day ORDER BY day`, fromDay, toDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UsageDay
	for rows.Next() {
		var u domain.UsageDay
		if err := rows.Scan(&u.Day, &u.Down, &u.Up, &u.Seconds); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// TopMembers returns up to limit members with the most traffic (download + upload) in the
// inclusive day range; ties by username. Members with neither traffic nor time are not listed.
func (s *Store) TopMembers(ctx context.Context, fromDay, toDay string, limit int) ([]domain.MemberUsage, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT m.id, m.username, SUM(u.down), SUM(u.up), SUM(u.seconds)
		FROM usage_daily u JOIN member m ON m.id = u.member_id
		WHERE u.day >= ? AND u.day <= ?
		GROUP BY m.id HAVING SUM(u.down) + SUM(u.up) + SUM(u.seconds) > 0
		ORDER BY SUM(u.down) + SUM(u.up) DESC, m.username LIMIT ?`, fromDay, toDay, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MemberUsage
	for rows.Next() {
		var u domain.MemberUsage
		if err := rows.Scan(&u.MemberID, &u.Username, &u.Down, &u.Up, &u.Seconds); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
