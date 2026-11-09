package store

import (
	"context"
	"database/sql"
	"errors"
)

// InsertAdminSession stores a new admin session by the hash of its token.
func (s *Store) InsertAdminSession(ctx context.Context, tokenHash, csrf string, createdAt, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_session(token_hash, csrf_token, created_at, expires_at) VALUES (?,?,?,?)`,
		tokenHash, csrf, createdAt, expiresAt)
	return err
}

// touchEvery is how old the stored expiry must be before it is slid again.
const touchEvery = 60 // seconds

// TouchAdminSession returns the CSRF token of a live session and whether it was found.
// It slides the expiry to newExpiresAt only when the stored one is at least touchEvery
// seconds behind, so most requests do not write.
func (s *Store) TouchAdminSession(ctx context.Context, tokenHash string, now, newExpiresAt int64) (string, bool, error) {
	var csrf string
	var exp int64
	err := s.r.QueryRowContext(ctx, `SELECT csrf_token, expires_at FROM admin_session WHERE token_hash=? AND expires_at>?`,
		tokenHash, now).Scan(&csrf, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if newExpiresAt-exp < touchEvery {
		return csrf, true, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admin_session SET expires_at=? WHERE token_hash=? AND expires_at>?`, newExpiresAt, tokenHash, now)
	if err != nil {
		return "", false, err
	}
	if n, _ := res.RowsAffected(); n == 0 { // deleted or expired in between
		return "", false, nil
	}
	return csrf, true, nil
}

// DeleteAdminSession removes the session with this token hash.
func (s *Store) DeleteAdminSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE token_hash=?`, tokenHash)
	return err
}

// PurgeExpiredAdminSessions deletes the sessions that expired at or before now.
func (s *Store) PurgeExpiredAdminSessions(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE expires_at<=?`, now)
	return err
}

// SetAdminPassword stores the new hash, clears must_change_password and deletes every admin session.
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

// ChangeAdminUsername stores the new username and deletes every admin session
// except keepTokenHash, atomically.
func (s *Store) ChangeAdminUsername(ctx context.Context, username string, now int64, keepTokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE admin SET username=?, updated_at=? WHERE id=1`, username, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_session WHERE token_hash<>?`, keepTokenHash); err != nil {
		return err
	}
	return tx.Commit()
}

// ChangeAdminPassword is SetAdminPassword for the web page. It also stores username
// (the same value when it does not change) and keeps the session keepTokenHash and
// deletes the others, all in one transaction.
func (s *Store) ChangeAdminPassword(ctx context.Context, hash, username string, now int64, keepTokenHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE admin SET password_hash=?, username=?, must_change_password=0, updated_at=? WHERE id=1`, hash, username, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_session WHERE token_hash<>?`, keepTokenHash); err != nil {
		return err
	}
	return tx.Commit()
}
