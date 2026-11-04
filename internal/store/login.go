package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// GetMemberForLogin finds a member by username (case-insensitive: the column
// is COLLATE NOCASE) and returns the encrypted password with it.
// domain.ErrNotFound when there is no such member.
func (s *Store) GetMemberForLogin(ctx context.Context, username string) (domain.Member, []byte, error) {
	var enc []byte
	m, err := scanMember(s.r.QueryRowContext(ctx, `SELECT `+memberCols+`, m.password_enc FROM member m WHERE m.username = ?`, username), nil, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil, domain.ErrNotFound
	}
	return m, enc, err
}

// ListActiveMembersByMAC returns the active members with this normalized MAC,
// newest first (highest id), each with its encrypted password. Several members
// may share a MAC.
func (s *Store) ListActiveMembersByMAC(ctx context.Context, mac string) ([]domain.Member, [][]byte, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT `+memberCols+`, m.password_enc FROM member m WHERE m.mac = ? AND m.mac <> '' AND m.active = 1 ORDER BY m.id DESC`, mac)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ms []domain.Member
	var encs [][]byte
	for rows.Next() {
		var enc []byte
		m, err := scanMember(rows, nil, &enc)
		if err != nil {
			return nil, nil, err
		}
		ms, encs = append(ms, m), append(encs, enc)
	}
	return ms, encs, rows.Err()
}

// CountActiveSessions counts the member's unended sessions updated at or after since (BR-9).
func (s *Store) CountActiveSessions(ctx context.Context, memberID int64, since time.Time) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM session s WHERE s.member_id = ? AND `+onlineSession,
		memberID, since.Unix()).Scan(&n)
	return n, err
}

// RenewMemberIfUnchanged is RenewMember (BR-13b) guarded by the window start the
// caller evaluated, so two simultaneous logins renew only once. It reports
// whether this call renewed the member.
func (s *Store) RenewMemberIfUnchanged(ctx context.Context, id int64, subscription time.Time, kind string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET subscription_date=?1, extra_bulk_bytes=0, used_download_bytes=0, used_upload_bytes=0,
		used_seconds=0, last_renewed_at=?1, renewal_count=renewal_count+1, last_renewal_kind=?2, updated_at=?1
		WHERE id=?3 AND subscription_date=?4`, now.Unix(), kind, id, subscription.Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
