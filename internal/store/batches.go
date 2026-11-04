package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// CreateBatch inserts the batch row and its members and advances settings.batch_counter
// in one transaction (BR-15), then returns the batch and the new usernames. The write
// lock is taken up front, so two creations never read the same counter. It fails the
// whole batch with *domain.UsernameConflictError when a username exists, and with
// domain.ErrPlanGone when the plan was deleted.
func (s *Store) CreateBatch(ctx context.Context, nb domain.NewBatch, now time.Time) (domain.Batch, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Batch{}, nil, err
	}
	defer tx.Rollback()
	var prefix string
	var counter int
	if err := tx.QueryRowContext(ctx, `SELECT batch_username_prefix, batch_counter FROM settings WHERE id=1`).Scan(&prefix, &counter); err != nil {
		return domain.Batch{}, nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO batch(label, plan_id, count, duration_days, expires_at, created_at) VALUES (?,?,?,?,?,?)`,
		nb.Input.Label, nb.Input.PlanID, nb.Input.Count, nb.DurationDays, nb.ExpiresAt.Unix(), now.Unix())
	if sqliteCode(err) == sqliteConstraintFK { // the plan was deleted after the service checked it
		return domain.Batch{}, nil, domain.ErrPlanGone
	}
	if err != nil {
		return domain.Batch{}, nil, err
	}
	batchID, err := res.LastInsertId()
	if err != nil {
		return domain.Batch{}, nil, err
	}
	usernames := make([]string, nb.Input.Count)
	for i, enc := range nb.PasswordsEnc {
		name := domain.BatchUsername(prefix, counter+i)
		_, err := tx.ExecContext(ctx, `INSERT INTO member(username, password_enc, active, plan_id, subscription_date, expires_at, batch_id, created_at, updated_at)
			VALUES (?,?,1,?,?,?,?,?,?)`, name, enc, nb.Input.PlanID, now.Unix(), nb.ExpiresAt.Unix(), batchID, now.Unix(), now.Unix())
		if sqliteCode(err) == sqliteConstraintUnique {
			return domain.Batch{}, nil, &domain.UsernameConflictError{Username: name}
		}
		if err != nil {
			return domain.Batch{}, nil, err
		}
		usernames[i] = name
	}
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET batch_counter = batch_counter + ?, updated_at=? WHERE id=1`, nb.Input.Count, now.Unix()); err != nil {
		return domain.Batch{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Batch{}, nil, err
	}
	return domain.Batch{ID: batchID, Label: nb.Input.Label, PlanID: nb.Input.PlanID, Count: nb.Input.Count,
		DurationDays: nb.DurationDays, ExpiresAt: nb.ExpiresAt.UTC(), CreatedAt: now.UTC()}, usernames, nil
}

// batchCols are the batch columns that scanBatch reads.
const batchCols = `b.id, b.label, b.plan_id, b.count, b.duration_days, b.expires_at, b.created_at`

// scanBatch reads batchCols, then extra.
func scanBatch(r rowScanner, extra ...any) (domain.Batch, error) {
	var b domain.Batch
	var exp, created int64
	err := r.Scan(append([]any{&b.ID, &b.Label, &b.PlanID, &b.Count, &b.DurationDays, &exp, &created}, extra...)...)
	b.ExpiresAt, b.CreatedAt = time.Unix(exp, 0).UTC(), time.Unix(created, 0).UTC()
	return b, err
}

// GetBatch returns one batch, or domain.ErrNotFound.
func (s *Store) GetBatch(ctx context.Context, id int64) (domain.Batch, error) {
	b, err := scanBatch(s.r.QueryRowContext(ctx, `SELECT `+batchCols+` FROM batch b WHERE b.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return b, domain.ErrNotFound
	}
	return b, err
}

// CountBatches returns the number of batches.
func (s *Store) CountBatches(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM batch`).Scan(&n)
	return n, err
}

// unusedMember is the SQL test for a member who never used the plan: all counters 0 and no session row (PLAN 8.2).
const unusedMember = `m.used_download_bytes = 0 AND m.used_upload_bytes = 0 AND m.used_seconds = 0
	AND NOT EXISTS (SELECT 1 FROM session x WHERE x.member_id = m.id)`

// ListBatches returns one page of batches, newest first, with member, unused and
// online counts (onlineExists, BR-17).
func (s *Store) ListBatches(ctx context.Context, limit, offset int, onlineSince time.Time) ([]domain.BatchRow, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+batchCols+`, p.name,
		(SELECT count(*) FROM member m WHERE m.batch_id = b.id),
		(SELECT count(*) FROM member m WHERE m.batch_id = b.id AND `+unusedMember+`),
		(SELECT count(*) FROM member m WHERE m.batch_id = b.id AND `+onlineExists+`)
		FROM batch b JOIN plan p ON p.id = b.plan_id ORDER BY b.id DESC LIMIT ? OFFSET ?`, onlineSince.Unix(), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BatchRow
	for rows.Next() {
		var r domain.BatchRow
		if r.Batch, err = scanBatch(rows, &r.PlanName, &r.Members, &r.Unused, &r.Online); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RenameBatch changes the label only (BR-23a). It returns domain.ErrNotFound for an unknown batch.
func (s *Store) RenameBatch(ctx context.Context, id int64, label string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE batch SET label=? WHERE id=?`, label, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// ListBatchCredentials returns username and encrypted password of the members now in the batch, oldest first.
func (s *Store) ListBatchCredentials(ctx context.Context, batchID int64) ([]domain.BatchCredential, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT username, password_enc FROM member WHERE batch_id=? ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BatchCredential
	for rows.Next() {
		var c domain.BatchCredential
		if err := rows.Scan(&c.Username, &c.PasswordEnc); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
