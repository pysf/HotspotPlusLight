package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"hotspotpluslight/internal/domain"
)

const memberCols = `m.id, m.username, m.active, m.first_name, m.last_name, m.mobile, m.email, m.national_code, m.mac, m.plan_id,
	m.subscription_date, m.extra_bulk_bytes, m.expires_at, m.batch_id, m.used_download_bytes, m.used_upload_bytes, m.used_seconds,
	m.last_renewed_at, m.renewal_count, m.last_renewal_kind, m.created_at`

const planColsJoined = `p.id, p.name, p.duration_days, p.speed_value, p.speed_unit, p.bulk_value, p.bulk_unit, p.time_limit_minutes, p.max_sessions, p.auto_resubscribe`

func optTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

func optUnix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// scanMember reads memberCols, then (if plan != nil) the joined plan columns, then extra.
func scanMember(r rowScanner, plan *domain.Plan, extra ...any) (domain.Member, error) {
	var m domain.Member
	var active, planAuto int
	var sub, created int64
	var exp, batch, renewed sql.NullInt64
	dest := []any{&m.ID, &m.Username, &active, &m.FirstName, &m.LastName, &m.Mobile, &m.Email, &m.NationalCode, &m.MAC, &m.PlanID,
		&sub, &m.ExtraBulkBytes, &exp, &batch, &m.UsedDownload, &m.UsedUpload, &m.UsedSeconds,
		&renewed, &m.RenewalCount, &m.LastRenewalKind, &created}
	if plan != nil {
		dest = append(dest, &plan.ID, &plan.Name, &plan.DurationDays, &plan.SpeedValue, &plan.SpeedUnit, &plan.BulkValue, &plan.BulkUnit,
			&plan.TimeLimitMinutes, &plan.MaxSessions, &planAuto)
	}
	if err := r.Scan(append(dest, extra...)...); err != nil {
		return m, err
	}
	if plan != nil {
		plan.AutoResubscribe = planAuto != 0
	}
	m.Active = active != 0
	m.SubscriptionDate = time.Unix(sub, 0).UTC()
	m.CreatedAt = time.Unix(created, 0).UTC()
	m.ExpiresAt, m.LastRenewedAt = optTime(exp), optTime(renewed)
	if batch.Valid {
		v := batch.Int64
		m.BatchID = &v
	}
	return m, nil
}

func (s *Store) CreateMember(ctx context.Context, in domain.MemberInput, passwordEnc []byte, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO member(username, password_enc, active, first_name, last_name, mobile, email, national_code,
		mac, plan_id, subscription_date, expires_at, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Username, passwordEnc, boolInt(in.Active), in.FirstName, in.LastName, in.Mobile, in.Email, in.NationalCode,
		in.MAC, in.PlanID, now.Unix(), optUnix(in.ExpiresAt), now.Unix(), now.Unix())
	if sqliteCode(err) == sqliteConstraintUnique {
		return 0, domain.ErrUsernameTaken
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetMember(ctx context.Context, id int64) (domain.Member, error) {
	m, err := scanMember(s.db.QueryRowContext(ctx, `SELECT `+memberCols+` FROM member m WHERE m.id=?`, id), nil)
	if errors.Is(err, sql.ErrNoRows) {
		return m, domain.ErrNotFound
	}
	return m, err
}

// UpdateMember changes the editable fields only: window, counters and extra bulk are untouched (BR-13a).
func (s *Store) UpdateMember(ctx context.Context, id int64, in domain.MemberInput, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET username=?, active=?, first_name=?, last_name=?, mobile=?, email=?, national_code=?,
		mac=?, plan_id=?, expires_at=?, updated_at=? WHERE id=?`,
		in.Username, boolInt(in.Active), in.FirstName, in.LastName, in.Mobile, in.Email, in.NationalCode,
		in.MAC, in.PlanID, optUnix(in.ExpiresAt), now.Unix(), id)
	if sqliteCode(err) == sqliteConstraintUnique {
		return domain.ErrUsernameTaken
	}
	if err != nil {
		return err
	}
	return affected(res)
}

func affected(res sql.Result) error {
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetMemberPassword(ctx context.Context, id int64) ([]byte, error) {
	var enc []byte
	err := s.db.QueryRowContext(ctx, `SELECT password_enc FROM member WHERE id=?`, id).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return enc, err
}

func (s *Store) SetMemberPassword(ctx context.Context, id int64, passwordEnc []byte, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET password_enc=?, updated_at=? WHERE id=?`, passwordEnc, now.Unix(), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// RenewMember restarts the validity window and clears extra bulk and counters (BR-13b).
func (s *Store) RenewMember(ctx context.Context, id int64, kind string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET subscription_date=?1, extra_bulk_bytes=0, used_download_bytes=0, used_upload_bytes=0,
		used_seconds=0, last_renewed_at=?1, renewal_count=renewal_count+1, last_renewal_kind=?2, updated_at=?1 WHERE id=?3`, now.Unix(), kind, id)
	if err != nil {
		return err
	}
	return affected(res)
}

func (s *Store) SetExtraBulk(ctx context.Context, id int64, bytes int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET extra_bulk_bytes=?, updated_at=? WHERE id=?`, bytes, now.Unix(), id)
	if err != nil {
		return err
	}
	return affected(res)
}

func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// filterClause builds the WHERE fragment (alias m) for a filter. Only fixed SQL
// text is concatenated; every value is a bound parameter.
func filterClause(f domain.MemberFilter, now time.Time) (string, []any) {
	var parts []string
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		pat := likePattern(q)
		parts = append(parts, `(m.username LIKE ? ESCAPE '\' OR m.first_name LIKE ? ESCAPE '\' OR m.last_name LIKE ? ESCAPE '\'
			OR (m.first_name || ' ' || m.last_name) LIKE ? ESCAPE '\' OR m.mobile LIKE ? ESCAPE '\')`)
		args = append(args, pat, pat, pat, pat, pat)
	}
	if f.PlanID > 0 {
		parts = append(parts, `m.plan_id = ?`)
		args = append(args, f.PlanID)
	}
	const expired = `(m.expires_at IS NOT NULL AND m.expires_at < ?)`
	switch f.Status {
	case domain.StatusExpired:
		parts = append(parts, expired)
		args = append(args, now.Unix())
	case domain.StatusActive:
		parts = append(parts, `m.active = 1 AND NOT `+expired)
		args = append(args, now.Unix())
	case domain.StatusInactive:
		parts = append(parts, `m.active = 0 AND NOT `+expired)
		args = append(args, now.Unix())
	}
	if len(parts) == 0 {
		return `1`, nil
	}
	return strings.Join(parts, ` AND `), args
}

// selectionClause is the WHERE fragment for explicit ids or a filter; an empty selection matches nothing.
func selectionClause(sel domain.MemberSelection, now time.Time) (string, []any) {
	switch {
	case sel.Filter != nil:
		return filterClause(*sel.Filter, now)
	case len(sel.IDs) > 0:
		args := make([]any, len(sel.IDs))
		for i, id := range sel.IDs {
			args[i] = id
		}
		return `m.id IN (?` + strings.Repeat(`,?`, len(sel.IDs)-1) + `)`, args
	}
	return `0`, nil
}

const onlineExists = `EXISTS (SELECT 1 FROM session s WHERE s.member_id = m.id AND s.ended_at IS NULL AND s.last_update_at >= ?)`

// CountFiltered counts the members matching a filter.
func (s *Store) CountFiltered(ctx context.Context, f domain.MemberFilter, now time.Time) (int, error) {
	where, args := filterClause(f, now)
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM member m WHERE `+where, args...).Scan(&n)
	return n, err
}

// ListMembers returns one page of members (newest first) with their plan and online flag.
func (s *Store) ListMembers(ctx context.Context, f domain.MemberFilter, limit, offset int, now, onlineSince time.Time) ([]domain.MemberRow, error) {
	where, args := filterClause(f, now)
	q := `SELECT ` + memberCols + `, ` + planColsJoined + `, ` + onlineExists + ` FROM member m JOIN plan p ON p.id = m.plan_id
		WHERE ` + where + ` ORDER BY m.id DESC LIMIT ? OFFSET ?`
	qargs := append([]any{onlineSince.Unix()}, args...)
	// The online column comes last in the select list but its parameter is the first "?" in the text.
	rows, err := s.db.QueryContext(ctx, q, append(qargs, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MemberRow
	for rows.Next() {
		var r domain.MemberRow
		var online int
		if r.Member, err = scanMember(rows, &r.Plan, &online); err != nil {
			return nil, err
		}
		r.Online = online != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetMemberRow returns one member with its plan and online flag.
func (s *Store) GetMemberRow(ctx context.Context, id int64, onlineSince time.Time) (domain.MemberRow, error) {
	var r domain.MemberRow
	var online int
	row := s.db.QueryRowContext(ctx, `SELECT `+memberCols+`, `+planColsJoined+`, `+onlineExists+` FROM member m JOIN plan p ON p.id = m.plan_id WHERE m.id = ?`,
		onlineSince.Unix(), id)
	m, err := scanMember(row, &r.Plan, &online)
	if errors.Is(err, sql.ErrNoRows) {
		return r, domain.ErrNotFound
	}
	r.Member, r.Online = m, online != 0
	return r, err
}

// SelectionStats counts the members of a selection and how many of them are online.
func (s *Store) SelectionStats(ctx context.Context, sel domain.MemberSelection, now, onlineSince time.Time) (members, online int, err error) {
	where, args := selectionClause(sel, now)
	err = s.db.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(`+onlineExists+`), 0) FROM member m WHERE `+where,
		append([]any{onlineSince.Unix()}, args...)...).Scan(&members, &online)
	return
}

// OpenSessions lists the open sessions (ended_at IS NULL) of the selected members.
func (s *Store) OpenSessions(ctx context.Context, sel domain.MemberSelection, now time.Time) ([]domain.SessionTarget, error) {
	where, args := selectionClause(sel, now)
	return openSessions(ctx, s.db, where, args)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func openSessions(ctx context.Context, q querier, where string, args []any) ([]domain.SessionTarget, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.nas_ip, s.username, s.acct_session_id, s.framed_ip, s.mac FROM session s
		JOIN member m ON m.id = s.member_id WHERE s.ended_at IS NULL AND (`+where+`) ORDER BY s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SessionTarget
	for rows.Next() {
		var t domain.SessionTarget
		if err := rows.Scan(&t.NASIP, &t.Username, &t.AcctSessionID, &t.FramedIP, &t.MAC); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteMembers deletes the selected members in one transaction (their session
// rows go with them through ON DELETE CASCADE). A filter is evaluated inside the
// transaction (Q29). It returns how many members were deleted and the open
// sessions they had at that moment, so the caller can kick any it missed.
func (s *Store) DeleteMembers(ctx context.Context, sel domain.MemberSelection, now time.Time) (int, []domain.SessionTarget, error) {
	where, args := selectionClause(sel, now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	open, err := openSessions(ctx, tx, where, args)
	if err != nil {
		return 0, nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM member WHERE id IN (SELECT m.id FROM member m WHERE `+where+`)`, args...)
	if err != nil {
		return 0, nil, err
	}
	n, _ := res.RowsAffected()
	return int(n), open, tx.Commit()
}
