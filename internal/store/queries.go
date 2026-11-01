package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// SeedInput is everything first-boot seeding writes.
type SeedInput struct {
	AdminUsername     string
	AdminPasswordHash string
	KeyCheck          string
	RadiusSecret      string
	Now               time.Time
}

// IsFirstBoot reports whether the settings row does not exist yet.
func (s *Store) IsFirstBoot(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM settings`).Scan(&n); err != nil {
		return false, err
	}
	return n == 0, nil
}

// FirstBoot seeds the admin and the settings row in ONE transaction, and only
// if no settings row exists. It returns true when it seeded. It creates no plan:
// the admin creates plans in the web UI (owner decision, E0.08).
func (s *Store) FirstBoot(ctx context.Context, in SeedInput) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM settings`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	now := in.Now.Unix()
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin(id, username, password_hash, updated_at) VALUES (1, ?, ?, ?)`,
		in.AdminUsername, in.AdminPasswordHash, now); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(id, key_check, radius_shared_secret, updated_at)
		VALUES (1, ?, ?, ?)`, in.KeyCheck, in.RadiusSecret, now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// KeyCheck returns settings.key_check; found is false when there is no row or it is empty.
func (s *Store) KeyCheck(ctx context.Context) (value string, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT key_check FROM settings WHERE id=1`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, value != "", err
}

// RadiusSecret is read per packet so settings changes apply without restart (PLAN 7.1).
func (s *Store) RadiusSecret(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT radius_shared_secret FROM settings WHERE id=1`).Scan(&v)
	return v, err
}

func (s *Store) GetAdmin(ctx context.Context) (domain.Admin, error) {
	var a domain.Admin
	err := s.db.QueryRowContext(ctx, `SELECT username, password_hash FROM admin WHERE id=1`).Scan(&a.Username, &a.PasswordHash)
	return a, err
}

func (s *Store) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, duration_days, speed_value, speed_unit, bulk_value, bulk_unit,
		time_limit_minutes, max_sessions, auto_resubscribe FROM plan ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Plan
	for rows.Next() {
		var p domain.Plan
		var auto int
		if err := rows.Scan(&p.ID, &p.Name, &p.DurationDays, &p.SpeedValue, &p.SpeedUnit, &p.BulkValue, &p.BulkUnit,
			&p.TimeLimitMinutes, &p.MaxSessions, &auto); err != nil {
			return nil, err
		}
		p.AutoResubscribe = auto != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CountMembers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM member`).Scan(&n)
	return n, err
}

func (s *Store) CountOpenSessions(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM session WHERE ended_at IS NULL`).Scan(&n)
	return n, err
}
