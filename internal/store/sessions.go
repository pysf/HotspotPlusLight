package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// CountOnlineSessions counts the online sessions (see onlineSession).
func (s *Store) CountOnlineSessions(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM session s WHERE `+onlineSession, since.Unix()).Scan(&n)
	return n, err
}

// ListOnlineSessions returns one page of online sessions, newest first.
func (s *Store) ListOnlineSessions(ctx context.Context, since time.Time, limit, offset int) ([]domain.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM session s WHERE `+onlineSession+` ORDER BY s.started_at DESC, s.id DESC LIMIT ? OFFSET ?`,
		since.Unix(), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SessionTargetByID returns what a Disconnect needs for one session row, and
// whether the row is still open. domain.ErrNotFound if there is no such row.
func (s *Store) SessionTargetByID(ctx context.Context, id int64) (domain.SessionTarget, bool, error) {
	var t domain.SessionTarget
	var ended sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, nas_ip, username, acct_session_id, framed_ip, mac, ended_at FROM session WHERE id = ?`, id).
		Scan(&t.ID, &t.NASIP, &t.Username, &t.AcctSessionID, &t.FramedIP, &t.MAC, &ended)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, domain.ErrNotFound
	}
	return t, !ended.Valid, err
}

// EndSession marks an open session ended (an ACKed kick, BR-18). Counters are
// untouched: the router's final Stop still completes the row (domain.ApplyAccounting).
func (s *Store) EndSession(ctx context.Context, id int64, now time.Time, cause string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE session SET ended_at = ?, terminate_cause = ? WHERE id = ? AND ended_at IS NULL`, now.Unix(), cause, id)
	return err
}

// OpenSessionTarget finds the open session of an accounting key (username,
// Acct-Session-Id, router address) for a Disconnect. The bool is false when no
// open row exists.
func (s *Store) OpenSessionTarget(ctx context.Context, username, acctSessionID, nasIP string) (domain.SessionTarget, bool, error) {
	var t domain.SessionTarget
	err := s.db.QueryRowContext(ctx, `SELECT id, nas_ip, username, acct_session_id, framed_ip, mac FROM session
		WHERE username=? AND acct_session_id=? AND nas_ip=? AND ended_at IS NULL ORDER BY id DESC LIMIT 1`, username, acctSessionID, nasIP).
		Scan(&t.ID, &t.NASIP, &t.Username, &t.AcctSessionID, &t.FramedIP, &t.MAC)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, nil
	}
	return t, err == nil, err
}
