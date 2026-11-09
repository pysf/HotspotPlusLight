package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// AcctTx is one accounting transaction. It satisfies session.AcctTx; internal/app
// adapts InAcctTx to session.Repo.
type AcctTx struct{ tx *sql.Tx }

// InAcctTx runs fn in one write transaction. It commits when fn returns nil and
// rolls back on any error. The caller answers the router only after it returns
// (PLAN 7.3, E4.15).
//
// While fn runs it holds the only write connection. So fn must use only tx, must
// not call another Store method (it would wait for itself), and must not wait for
// another request. The same holds for every transaction on s.db in this package.
func (s *Store) InAcctTx(ctx context.Context, fn func(tx *AcctTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&AcctTx{tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// Settings returns the settings row, read inside the transaction.
func (t *AcctTx) Settings(ctx context.Context) (domain.Settings, error) {
	return scanSettings(t.tx.QueryRowContext(ctx, settingsSelect))
}

// MemberByUsername returns the member, or domain.ErrNotFound.
func (t *AcctTx) MemberByUsername(ctx context.Context, username string) (domain.Member, error) {
	m, err := scanMember(t.tx.QueryRowContext(ctx, `SELECT `+memberCols+` FROM member m WHERE m.username = ?`, username), nil)
	if errors.Is(err, sql.ErrNoRows) {
		return m, domain.ErrNotFound
	}
	return m, err
}

// SetMemberMACIfEmpty stores mac on the member only if it has none yet.
func (t *AcctTx) SetMemberMACIfEmpty(ctx context.Context, id int64, mac string, now time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE member SET mac=?, updated_at=? WHERE id=? AND mac=''`, mac, now.Unix(), id)
	return err
}

const sessionCols = `id, member_id, username, acct_session_id, nas_ip, nas_identifier, framed_ip, mac, status, started_at,
	last_update_at, ended_at, terminate_cause, download_bytes, upload_bytes, session_seconds`

// LatestSession returns the latest open row of the identity key (username, session
// id, NAS IP), else the latest finished one. It returns nil when there is none.
func (t *AcctTx) LatestSession(ctx context.Context, username, sessionID, nasIP string) (*domain.Session, error) {
	for _, open := range []string{" AND ended_at IS NULL", " AND ended_at IS NOT NULL"} {
		sess, err := scanSession(t.tx.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM session WHERE username=? AND acct_session_id=? AND nas_ip=?`+open+
			` ORDER BY id DESC LIMIT 1`, username, sessionID, nasIP))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &sess, nil
	}
	return nil, nil
}

// scanSession reads one row selected with sessionCols.
func scanSession(row rowScanner) (domain.Session, error) {
	var s domain.Session
	var status int
	var started, last int64
	var ended sql.NullInt64
	err := row.Scan(&s.ID, &s.MemberID, &s.Username, &s.AcctSessionID, &s.NASIP, &s.NASIdentifier, &s.FramedIP, &s.MAC, &status,
		&started, &last, &ended, &s.TerminateCause, &s.Cum.Download, &s.Cum.Upload, &s.Cum.Seconds)
	s.Status = domain.AcctStatus(status)
	s.StartedAt, s.LastUpdateAt, s.EndedAt = time.Unix(started, 0).UTC(), time.Unix(last, 0).UTC(), optTime(ended)
	return s, err
}

// InsertSession adds a session row.
func (t *AcctTx) InsertSession(ctx context.Context, s domain.Session) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO session(member_id, username, acct_session_id, nas_ip, nas_identifier, framed_ip, mac, status,
		started_at, last_update_at, ended_at, terminate_cause, download_bytes, upload_bytes, session_seconds)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.MemberID, s.Username, s.AcctSessionID, s.NASIP, s.NASIdentifier, s.FramedIP, s.MAC, int(s.Status),
		s.StartedAt.Unix(), s.LastUpdateAt.Unix(), optUnix(s.EndedAt), s.TerminateCause, s.Cum.Download, s.Cum.Upload, s.Cum.Seconds)
	return err
}

// UpdateSession writes the changeable fields of the session with s.ID.
func (t *AcctTx) UpdateSession(ctx context.Context, s domain.Session) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE session SET nas_identifier=?, framed_ip=?, mac=?, status=?, last_update_at=?, ended_at=?,
		terminate_cause=?, download_bytes=?, upload_bytes=?, session_seconds=? WHERE id=?`,
		s.NASIdentifier, s.FramedIP, s.MAC, int(s.Status), s.LastUpdateAt.Unix(), optUnix(s.EndedAt), s.TerminateCause,
		s.Cum.Download, s.Cum.Upload, s.Cum.Seconds, s.ID)
	return err
}

// AddUsage adds d to the member's counters in SQL, so it is relative to what the
// row holds inside this transaction (BR-11, BR-13b).
func (t *AcctTx) AddUsage(ctx context.Context, memberID int64, d domain.Counters, now time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE member SET used_download_bytes=min(used_download_bytes+?1, ?4), used_upload_bytes=min(used_upload_bytes+?2, ?4),
		used_seconds=used_seconds+?3, updated_at=?5 WHERE id=?6`, d.Download, d.Upload, d.Seconds, domain.MaxUsedBytes, now.Unix(), memberID)
	return err
}

// AddDailyUsage adds d to the member's usage_daily row for day, creating it if needed (D12).
func (t *AcctTx) AddDailyUsage(ctx context.Context, memberID int64, day string, d domain.Counters) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO usage_daily(member_id, day, down, up, seconds) VALUES (?,?,?,?,?)
		ON CONFLICT(member_id, day) DO UPDATE SET down=min(down+excluded.down, ?6), up=min(up+excluded.up, ?6), seconds=seconds+excluded.seconds`,
		memberID, day, d.Download, d.Upload, d.Seconds, domain.MaxUsedBytes)
	return err
}

// pruneChunk is how many rows one pruning statement deletes. Small chunks let
// accounting writes in between, because the write connection is the only one.
const pruneChunk = 1000

// PruneSessions deletes finished sessions that ended before cutoff, and open
// ones that have been silent since before cutoff (their Stop never came). It
// returns how many rows went. It deletes in chunks so other writes can go in.
func (s *Store) PruneSessions(ctx context.Context, cutoff time.Time) (int, error) {
	return s.pruneSessions(ctx, cutoff, pruneChunk, nil)
}

// pruneSessions is PruneSessions with a chunk size and a hook that runs after each chunk.
func (s *Store) pruneSessions(ctx context.Context, cutoff time.Time, chunk int, afterChunk func()) (int, error) {
	total := 0
	for {
		res, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE id IN
			(SELECT id FROM session WHERE COALESCE(ended_at, last_update_at) < ? LIMIT ?)`, cutoff.Unix(), chunk)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += int(n)
		if afterChunk != nil {
			afterChunk()
		}
		if int(n) < chunk {
			return total, nil
		}
	}
}
