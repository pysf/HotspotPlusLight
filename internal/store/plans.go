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
// several extended codes (787 and 1811 seen), so isConstraint tests the primary
// code SQLITE_CONSTRAINT (19).
const (
	sqliteConstraint       = 19
	sqliteConstraintUnique = 2067
	sqliteConstraintFK     = 787
)

// isConstraint reports whether err is any SQLite constraint violation.
func isConstraint(err error) bool { return sqliteCode(err)&0xff == sqliteConstraint }

// sqliteCode returns the SQLite result code of err, or 0 if err is not a SQLite error.
func sqliteCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

// planCols are the plan columns that scanPlan reads.
const planCols = `id, name, duration_days, speed_value, speed_unit, bulk_value, bulk_unit, time_limit_minutes, max_sessions, auto_resubscribe`

// rowScanner is what the scan helpers need from *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(...any) error }

// scanPlan reads one row selected with planCols.
func scanPlan(r rowScanner) (domain.Plan, error) {
	var p domain.Plan
	var auto int
	err := r.Scan(&p.ID, &p.Name, &p.DurationDays, &p.SpeedValue, &p.SpeedUnit, &p.BulkValue, &p.BulkUnit,
		&p.TimeLimitMinutes, &p.MaxSessions, &auto)
	p.AutoResubscribe = auto != 0
	return p, err
}

// ListPlans returns all plans ordered by name.
func (s *Store) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+planCols+` FROM plan ORDER BY name COLLATE NOCASE, id`)
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

// GetPlan returns the plan, or domain.ErrNotFound.
func (s *Store) GetPlan(ctx context.Context, id int64) (domain.Plan, error) {
	p, err := scanPlan(s.r.QueryRowContext(ctx, `SELECT `+planCols+` FROM plan WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, domain.ErrNotFound
	}
	return p, err
}

// CreatePlan inserts a plan and returns its id, or domain.ErrNameTaken.
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

// UpdatePlan saves the plan with p.ID. It returns domain.ErrNotFound or domain.ErrNameTaken.
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

// DeletePlan deletes the plan. It returns domain.ErrNotFound for an unknown plan,
// domain.ErrDefault for the default plan, and *domain.InUseError (member and batch
// counts) when members or batches use it (O12). The default check is in the same
// statement, so a concurrent settings save cannot be undone; the RESTRICT foreign key
// catches a member that appears meanwhile.
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM plan WHERE id=?1
		AND NOT EXISTS (SELECT 1 FROM settings WHERE id=1 AND default_plan_id=?1)`, id)
	if isConstraint(err) {
		if txIsDefault(ctx, tx, id) {
			return domain.ErrDefault
		}
		var n, b int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM member WHERE plan_id=?1), (SELECT count(*) FROM batch WHERE plan_id=?1)`, id).Scan(&n, &b); err != nil {
			return err
		}
		return &domain.InUseError{Members: n, Batches: b}
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if txIsDefault(ctx, tx, id) {
			return domain.ErrDefault
		}
		return domain.ErrNotFound
	}
	return tx.Commit()
}

// txIsDefault reports whether the plan is the default plan, inside tx.
func txIsDefault(ctx context.Context, tx *sql.Tx, id int64) bool {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE id=1 AND default_plan_id=?`, id).Scan(&n)
	return err == nil && n > 0
}

// CountMembersOnPlan returns how many members use the plan.
func (s *Store) CountMembersOnPlan(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM member WHERE plan_id=?`, id).Scan(&n)
	return n, err
}

// IsDefaultPlan reports whether the plan is the default plan.
func (s *Store) IsDefaultPlan(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM settings WHERE id=1 AND default_plan_id=?`, id).Scan(&n)
	return n > 0, err
}

// PlanExists reports whether a plan with this id exists.
func (s *Store) PlanExists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM plan WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// PlanUsage returns, per plan id, how many members and batches use it. Plans without use are absent.
func (s *Store) PlanUsage(ctx context.Context) (map[int64]domain.PlanUsage, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT id, (SELECT count(*) FROM member WHERE plan_id=plan.id), (SELECT count(*) FROM batch WHERE plan_id=plan.id) FROM plan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]domain.PlanUsage{}
	for rows.Next() {
		var id int64
		var u domain.PlanUsage
		if err := rows.Scan(&id, &u.Members, &u.Batches); err != nil {
			return nil, err
		}
		out[id] = u
	}
	return out, rows.Err()
}
