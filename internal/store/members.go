package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"hotspotpluslight/internal/domain"
)

// memberCols are the member columns that scanMember reads.
const memberCols = `m.id, m.username, m.active, m.first_name, m.last_name, m.mobile, m.email, m.national_code, m.mac, m.plan_id,
	m.subscription_date, m.extra_bulk_bytes, m.expires_at, m.batch_id, m.used_download_bytes, m.used_upload_bytes, m.used_seconds,
	m.last_renewed_at, m.renewal_count, m.last_renewal_kind, m.created_at`

// planColsJoined are the plan columns that scanMember reads when it gets a plan.
const planColsJoined = `p.id, p.name, p.duration_days, p.speed_value, p.speed_unit, p.bulk_value, p.bulk_unit, p.time_limit_minutes, p.max_sessions, p.auto_resubscribe`

// optTime converts a nullable Unix time; NULL becomes nil.
func optTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

// optUnix converts an optional time to a Unix value for SQL; nil becomes NULL.
func optUnix(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// scanMember reads memberCols, then planColsJoined if plan is not nil, then extra.
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

// CreateMember inserts a member and returns its id, or domain.ErrUsernameTaken.
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

// GetMember returns the member, or domain.ErrNotFound.
func (s *Store) GetMember(ctx context.Context, id int64) (domain.Member, error) {
	m, err := scanMember(s.r.QueryRowContext(ctx, `SELECT `+memberCols+` FROM member m WHERE m.id=?`, id), nil)
	if errors.Is(err, sql.ErrNoRows) {
		return m, domain.ErrNotFound
	}
	return m, err
}

// UpdateMember changes the editable fields only; window, counters and extra bulk stay (BR-13a).
// It returns domain.ErrNotFound or domain.ErrUsernameTaken.
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

// SetMemberActive changes only the active flag. It returns domain.ErrNotFound for an unknown member.
func (s *Store) SetMemberActive(ctx context.Context, id int64, active bool, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET active=?, updated_at=? WHERE id=?`, boolInt(active), now.Unix(), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// affected returns domain.ErrNotFound when the statement changed no row.
func affected(res sql.Result) error {
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetMemberPassword returns the encrypted password, or domain.ErrNotFound.
func (s *Store) GetMemberPassword(ctx context.Context, id int64) ([]byte, error) {
	var enc []byte
	err := s.r.QueryRowContext(ctx, `SELECT password_enc FROM member WHERE id=?`, id).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return enc, err
}

// SetMemberPassword stores a new encrypted password. It returns domain.ErrNotFound for an unknown member.
func (s *Store) SetMemberPassword(ctx context.Context, id int64, passwordEnc []byte, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET password_enc=?, updated_at=? WHERE id=?`, passwordEnc, now.Unix(), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// RenewMember restarts the validity window and clears extra bulk and counters (BR-13b).
// It returns domain.ErrNotFound for an unknown member.
func (s *Store) RenewMember(ctx context.Context, id int64, kind string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET subscription_date=?1, extra_bulk_bytes=0, used_download_bytes=0, used_upload_bytes=0,
		used_seconds=0, last_renewed_at=?1, renewal_count=renewal_count+1, last_renewal_kind=?2, updated_at=?1 WHERE id=?3`, now.Unix(), kind, id)
	if err != nil {
		return err
	}
	return affected(res)
}

// SetExtraBulk sets the member's extra bulk in bytes. It returns domain.ErrNotFound for an unknown member.
func (s *Store) SetExtraBulk(ctx context.Context, id int64, bytes int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE member SET extra_bulk_bytes=?, updated_at=? WHERE id=?`, bytes, now.Unix(), id)
	if err != nil {
		return err
	}
	return affected(res)
}

// likePattern returns a LIKE pattern that matches q anywhere, with q's wildcards escaped.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// statusClause returns the SQL form of domain.MemberStatus (BR-24) and its arguments.
// The four statuses exclude each other and cover every member. An unknown status matches nothing.
func statusClause(status string, now time.Time) (string, []any) {
	n := now.Unix()
	soon := domain.ExpiryLimit(now, domain.MemberExpiringDays)
	switch status {
	case domain.StatusExpired:
		return `(m.expires_at IS NOT NULL AND m.expires_at < ?)`, []any{n}
	case domain.StatusInactive:
		return `(m.active = 0 AND (m.expires_at IS NULL OR m.expires_at >= ?))`, []any{n}
	case domain.StatusExpiring:
		return `(m.active = 1 AND m.expires_at IS NOT NULL AND m.expires_at >= ? AND m.expires_at <= ?)`, []any{n, soon}
	case domain.StatusActive:
		return `(m.active = 1 AND (m.expires_at IS NULL OR m.expires_at > ?))`, []any{soon}
	}
	return `0`, nil
}

// filterClause returns the WHERE fragment (table alias m) for a filter and its
// arguments. Only fixed SQL text is joined; every value is a bound parameter.
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
	if f.Status != "" {
		c, a := statusClause(f.Status, now)
		parts = append(parts, c)
		args = append(args, a...)
	}
	if len(parts) == 0 {
		return `1`, nil
	}
	return strings.Join(parts, ` AND `), args
}

// selectionClause returns the WHERE fragment for a selection (a batch, a filter or ids). An empty selection matches nothing.
func selectionClause(sel domain.MemberSelection, now time.Time) (string, []any) {
	switch {
	case sel.BatchID != nil:
		return `m.batch_id = ?`, []any{*sel.BatchID}
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

// onlineSession is the one definition of an online session (BR-9, BR-17): not ended and
// updated since the cut-off (its one parameter, domain.ActiveSince). The member list,
// the concurrent-session check and the online page all use it.
const onlineSession = `s.ended_at IS NULL AND s.last_update_at >= ?`

// onlineExists is the SQL test that member m has an online session; it takes the same parameter.
const onlineExists = `EXISTS (SELECT 1 FROM session s WHERE s.member_id = m.id AND ` + onlineSession + `)`

// CountFiltered returns the number of members matching a filter.
func (s *Store) CountFiltered(ctx context.Context, f domain.MemberFilter, now time.Time) (int, error) {
	where, args := filterClause(f, now)
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM member m WHERE `+where, args...).Scan(&n)
	return n, err
}

// CountStatuses returns, for the members matching the query and plan of a filter, the
// total and the count per status. The filter's Status is ignored (the list chips).
func (s *Store) CountStatuses(ctx context.Context, f domain.MemberFilter, now time.Time) (domain.StatusCounts, error) {
	f.Status = ""
	where, wargs := filterClause(f, now)
	var cols []string
	var args []any
	for _, st := range []string{domain.StatusActive, domain.StatusExpiring, domain.StatusExpired, domain.StatusInactive} {
		c, a := statusClause(st, now)
		cols = append(cols, `coalesce(sum(CASE WHEN `+c+` THEN 1 ELSE 0 END), 0)`)
		args = append(args, a...)
	}
	var c domain.StatusCounts
	err := s.r.QueryRowContext(ctx, `SELECT count(*), `+strings.Join(cols, `, `)+` FROM member m WHERE `+where,
		append(args, wargs...)...).Scan(&c.All, &c.Active, &c.Expiring, &c.Expired, &c.Inactive)
	return c, err
}

// ListMembers returns one page of members, newest first, with plan and online flag.
func (s *Store) ListMembers(ctx context.Context, f domain.MemberFilter, limit, offset int, now, onlineSince time.Time) ([]domain.MemberRow, error) {
	where, args := filterClause(f, now)
	q := `SELECT ` + memberCols + `, ` + planColsJoined + `, ` + onlineExists + ` FROM member m JOIN plan p ON p.id = m.plan_id
		WHERE ` + where + ` ORDER BY m.id DESC LIMIT ? OFFSET ?`
	qargs := append([]any{onlineSince.Unix()}, args...)
	// The online column comes last in the select list but its parameter is the first "?" in the text.
	rows, err := s.r.QueryContext(ctx, q, append(qargs, limit, offset)...)
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

// GetMemberRow returns one member with plan and online flag, or domain.ErrNotFound.
func (s *Store) GetMemberRow(ctx context.Context, id int64, onlineSince time.Time) (domain.MemberRow, error) {
	var r domain.MemberRow
	var online int
	row := s.r.QueryRowContext(ctx, `SELECT `+memberCols+`, `+planColsJoined+`, `+onlineExists+` FROM member m JOIN plan p ON p.id = m.plan_id WHERE m.id = ?`,
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
	err = s.r.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(`+onlineExists+`), 0) FROM member m WHERE `+where,
		append([]any{onlineSince.Unix()}, args...)...).Scan(&members, &online)
	return
}

// OpenSessions returns the open sessions of the selected members. It reads on the write
// connection, because a kick must see every commit made before it. The display counts
// use the read pool, where a tiny mismatch is fine.
func (s *Store) OpenSessions(ctx context.Context, sel domain.MemberSelection, now time.Time) ([]domain.SessionTarget, error) {
	where, args := selectionClause(sel, now)
	return openSessions(ctx, s.db, where, args)
}

// querier is what openSessions needs from a *sql.DB or a *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// openSessions returns the open sessions of the members matched by where.
func openSessions(ctx context.Context, q querier, where string, args []any) ([]domain.SessionTarget, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.id, s.nas_ip, s.username, s.acct_session_id, s.framed_ip, s.mac FROM session s
		JOIN member m ON m.id = s.member_id WHERE s.ended_at IS NULL AND (`+where+`) ORDER BY s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SessionTarget
	for rows.Next() {
		var t domain.SessionTarget
		if err := rows.Scan(&t.ID, &t.NASIP, &t.Username, &t.AcctSessionID, &t.FramedIP, &t.MAC); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteMembers deletes the selected members, with their sessions (ON DELETE CASCADE),
// in one transaction. A filter is evaluated inside it (Q29). It returns how many members
// went and the open sessions they had, so the caller can kick them.
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
	if sel.BatchID != nil { // the batch row goes in the same transaction (BR-23b)
		if _, err := tx.ExecContext(ctx, `DELETE FROM batch WHERE id=?`, *sel.BatchID); err != nil {
			return 0, nil, err
		}
	}
	return int(n), open, tx.Commit()
}
