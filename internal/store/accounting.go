package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// AcctTx is one accounting transaction; it satisfies session.AcctTx (the
// composition root adapts InAcctTx to session.Repo).
type AcctTx struct{ tx *sql.Tx }

// InAcctTx runs fn in ONE write transaction and commits when it returns nil;
// any error rolls everything back. The caller answers the router only after
// this returns (PLAN 7.3, E4.15).
//
// RULE (one write connection): while fn runs it holds the only write
// connection. fn must use only tx. It must never call another Store method
// (they need that connection or wait behind it), never use *sql.Rows from the
// write pool after returning, and never wait for another request. The same rule
// holds for every other transaction in this package (BeginTx on s.db).
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

func (t *AcctTx) MemberByUsername(ctx context.Context, username string) (domain.Member, error) {
	m, err := scanMember(t.tx.QueryRowContext(ctx, `SELECT `+memberCols+` FROM member m WHERE m.username = ?`, username), nil)
	if errors.Is(err, sql.ErrNoRows) {
		return m, domain.ErrNotFound
	}
	return m, err
}

func (t *AcctTx) SetMemberMACIfEmpty(ctx context.Context, id int64, mac string, now time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE member SET mac=?, updated_at=? WHERE id=? AND mac=''`, mac, now.Unix(), id)
	return err
}

const sessionCols = `id, member_id, username, acct_session_id, nas_ip, nas_identifier, framed_ip, mac, status, started_at,
	last_update_at, ended_at, terminate_cause, download_bytes, upload_bytes, session_seconds`

// LatestSession returns the latest open row of the identity key, else the
// latest finished one; nil when there is none. The first query uses the partial
// index session_open_key. MONITOR: the second query has no usable index (the
// only one is partial; a new one needs a migration and E0.09 counts them): a full
// scan, only when there is no open row. Add an index migration if it ever matters.
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

func (t *AcctTx) InsertSession(ctx context.Context, s domain.Session) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO session(member_id, username, acct_session_id, nas_ip, nas_identifier, framed_ip, mac, status,
		started_at, last_update_at, ended_at, terminate_cause, download_bytes, upload_bytes, session_seconds)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.MemberID, s.Username, s.AcctSessionID, s.NASIP, s.NASIdentifier, s.FramedIP, s.MAC, int(s.Status),
		s.StartedAt.Unix(), s.LastUpdateAt.Unix(), optUnix(s.EndedAt), s.TerminateCause, s.Cum.Download, s.Cum.Upload, s.Cum.Seconds)
	return err
}

func (t *AcctTx) UpdateSession(ctx context.Context, s domain.Session) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE session SET nas_identifier=?, framed_ip=?, mac=?, status=?, last_update_at=?, ended_at=?,
		terminate_cause=?, download_bytes=?, upload_bytes=?, session_seconds=? WHERE id=?`,
		s.NASIdentifier, s.FramedIP, s.MAC, int(s.Status), s.LastUpdateAt.Unix(), optUnix(s.EndedAt), s.TerminateCause,
		s.Cum.Download, s.Cum.Upload, s.Cum.Seconds, s.ID)
	return err
}

// AddUsage adds to the member's counters in SQL, so it is relative to whatever the
// row holds inside this transaction (BR-11, BR-13b).
func (t *AcctTx) AddUsage(ctx context.Context, memberID int64, d domain.Counters, now time.Time) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE member SET used_download_bytes=used_download_bytes+?, used_upload_bytes=used_upload_bytes+?,
		used_seconds=used_seconds+?, updated_at=? WHERE id=?`, d.Download, d.Upload, d.Seconds, now.Unix(), memberID)
	return err
}

// AddDailyUsage upserts the delta into the member's day row, in the packet's transaction (D12).
func (t *AcctTx) AddDailyUsage(ctx context.Context, memberID int64, day string, d domain.Counters) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO usage_daily(member_id, day, down, up, seconds) VALUES (?,?,?,?,?)
		ON CONFLICT(member_id, day) DO UPDATE SET down=down+excluded.down, up=up+excluded.up, seconds=seconds+excluded.seconds`,
		memberID, day, d.Download, d.Upload, d.Seconds)
	return err
}

// SessionRetentionDays is read at each pruning run, so the setting applies without restart (O18).
func (s *Store) SessionRetentionDays(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT session_retention_days FROM settings WHERE id=1`).Scan(&n)
	return n, err
}

// PruneSessions deletes finished sessions that ended before cutoff, and open
// ones that have been silent since before cutoff (their Stop never came). It
// returns how many rows went.
func (s *Store) PruneSessions(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE COALESCE(ended_at, last_update_at) < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
