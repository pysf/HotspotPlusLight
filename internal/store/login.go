package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"hotspotpluslight/internal/domain"
)

// GetMemberForLogin returns the member with this username (case-insensitive) and the
// encrypted password. It returns domain.ErrNotFound when there is no such member.
func (s *Store) GetMemberForLogin(ctx context.Context, username string) (domain.Member, []byte, error) {
	var enc []byte
	m, err := scanMember(s.r.QueryRowContext(ctx, `SELECT `+memberCols+`, m.password_enc FROM member m WHERE m.username = ?`, username), nil, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil, domain.ErrNotFound
	}
	return m, enc, err
}

// CountActiveSessions returns how many of the member's sessions are open and were updated at or after since (BR-9).
func (s *Store) CountActiveSessions(ctx context.Context, memberID int64, since time.Time) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM session s WHERE s.member_id = ? AND `+onlineSession,
		memberID, since.Unix()).Scan(&n)
	return n, err
}

// RenewMemberIfUnchanged is RenewMember (BR-13b) guarded by the window start the
// caller saw, so two simultaneous logins renew once. It reports whether this call renewed.
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
