package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"

	"hotspotpluslight/internal/domain"
)

// SQLite result codes (sqlite3.h). The driver reports RESTRICT violations with
// several extended codes (787 and 1811 seen), so foreign-key failures are
// recognised by the primary code SQLITE_CONSTRAINT (19).
const (
	sqliteConstraint       = 19
	sqliteConstraintUnique = 2067
)

func isConstraint(err error) bool { return sqliteCode(err)&0xff == sqliteConstraint }

func sqliteCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

const planCols = `id, name, duration_days, speed_value, speed_unit, bulk_value, bulk_unit, time_limit_minutes, max_sessions, auto_resubscribe`

type rowScanner interface{ Scan(...any) error }

func scanPlan(r rowScanner) (domain.Plan, error) {
	var p domain.Plan
	var auto int
	err := r.Scan(&p.ID, &p.Name, &p.DurationDays, &p.SpeedValue, &p.SpeedUnit, &p.BulkValue, &p.BulkUnit,
		&p.TimeLimitMinutes, &p.MaxSessions, &auto)
	p.AutoResubscribe = auto != 0
	return p, err
}

func (s *Store) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+planCols+` FROM plan ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetPlan(ctx context.Context, id int64) (domain.Plan, error) {
	p, err := scanPlan(s.db.QueryRowContext(ctx, `SELECT `+planCols+` FROM plan WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	return p, err
}

func (s *Store) CreatePlan(ctx context.Context, p domain.Plan, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO plan(name, duration_days, speed_value, speed_unit, bulk_value, bulk_unit,
		time_limit_minutes, max_sessions, auto_resubscribe, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.Name, p.DurationDays, p.SpeedValue, p.SpeedUnit, p.BulkValue, p.BulkUnit, p.TimeLimitMinutes, p.MaxSessions,
		boolInt(p.AutoResubscribe), now.Unix(), now.Unix())
	if sqliteCode(err) == sqliteConstraintUnique {
		return 0, domain.ErrNameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePlan(ctx context.Context, p domain.Plan, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE plan SET name=?, duration_days=?, speed_value=?, speed_unit=?, bulk_value=?, bulk_unit=?,
		time_limit_minutes=?, max_sessions=?, auto_resubscribe=?, updated_at=? WHERE id=?`,
		p.Name, p.DurationDays, p.SpeedValue, p.SpeedUnit, p.BulkValue, p.BulkUnit, p.TimeLimitMinutes, p.MaxSessions,
		boolInt(p.AutoResubscribe), now.Unix(), p.ID)
	if sqliteCode(err) == sqliteConstraintUnique {
		return domain.ErrNameTaken
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeletePlan deletes the plan unless it is the settings default (checked in the
// same statement, so a concurrent settings save cannot be undone silently). A
// member row that appeared meanwhile is caught by the RESTRICT foreign key.
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM plan WHERE id=?1
		AND NOT EXISTS (SELECT 1 FROM settings WHERE id=1 AND default_plan_id=?1)`, id)
	if isConstraint(err) {
		if def, derr := s.IsDefaultPlan(ctx, id); derr == nil && def {
			return domain.ErrDefault
		}
		n, _ := s.CountMembersOnPlan(ctx, id)
		return &domain.InUseError{Members: n}
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if def, derr := s.IsDefaultPlan(ctx, id); derr == nil && def {
			return domain.ErrDefault
		}
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) CountMembersOnPlan(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM member WHERE plan_id=?`, id).Scan(&n)
	return n, err
}

func (s *Store) IsDefaultPlan(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE id=1 AND default_plan_id=?`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) PlanExists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM plan WHERE id=?`, id).Scan(&n)
	return n > 0, err
}
