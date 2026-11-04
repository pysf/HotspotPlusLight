package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) InsertAdminSession(ctx context.Context, tokenHash, csrf string, createdAt, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_session(token_hash, csrf_token, created_at, expires_at) VALUES (?,?,?,?)`,
		tokenHash, csrf, createdAt, expiresAt)
	return err
}

// TouchAdminSession slides expiry of a live session in one statement and returns its CSRF token.
func (s *Store) TouchAdminSession(ctx context.Context, tokenHash string, now, newExpiresAt int64) (string, bool, error) {
	var csrf string
	err := s.db.QueryRowContext(ctx, `UPDATE admin_session SET expires_at=? WHERE token_hash=? AND expires_at>? RETURNING csrf_token`,
		newExpiresAt, tokenHash, now).Scan(&csrf)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return csrf, err == nil, err
}

func (s *Store) DeleteAdminSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE token_hash=?`, tokenHash)
	return err
}

func (s *Store) PurgeExpiredAdminSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE expires_at<=?`, now)
	return err
}

// SetAdminPassword updates the hash, clears the must-change flag and deletes every admin session in one transaction.
func (s *Store) SetAdminPassword(ctx context.Context, hash string, now int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE admin SET password_hash=?, must_change_password=0, updated_at=? WHERE id=1`, hash, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_session`); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeAdminPassword is SetAdminPassword for the web page: it keeps the session
// keepTokenHash and deletes all the others.
func (s *Store) ChangeAdminPassword(ctx context.Context, hash string, now int64, keepTokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE admin SET password_hash=?, must_change_password=0, updated_at=? WHERE id=1`, hash, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_session WHERE token_hash<>?`, keepTokenHash); err != nil {
		return err
	}
	return tx.Commit()
}
