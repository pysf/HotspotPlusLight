package store

import (
	"context"
	"database/sql"
	"time"

	"hotspotpluslight/internal/domain"
)

// settingsSelect reads the single settings row; scanSettings reads its result.
const settingsSelect = `SELECT business_name, timezone, radius_shared_secret, coa_port, accounting_interval_seconds,
		default_plan_id, batch_username_prefix, batch_counter, batch_max_count,
		voucher_password_length, voucher_help, session_retention_days,
		admin_addr, radius_auth_addr, radius_acct_addr, log_level, log_format FROM settings WHERE id=1`

// GetSettings returns the single settings row.
func (s *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	return scanSettings(s.r.QueryRowContext(ctx, settingsSelect))
}

// scanSettings reads the settings row selected with settingsSelect.
func scanSettings(row *sql.Row) (domain.Settings, error) {
	var st domain.Settings
	var def sql.NullInt64
	err := row.Scan(&st.BusinessName, &st.Timezone, &st.RadiusSharedSecret, &st.CoaPort, &st.AccountingIntervalSeconds, &def,
		&st.BatchUsernamePrefix, &st.BatchCounter, &st.BatchMaxCount, &st.VoucherPasswordLength,
		&st.VoucherHelp, &st.SessionRetentionDays,
		&st.AdminAddr, &st.RadiusAuthAddr, &st.RadiusAcctAddr, &st.LogLevel, &st.LogFormat)
	if def.Valid {
		v := def.Int64
		st.DefaultPlanID = &v
	}
	return st, err
}

// UpdateSettings saves every editable setting. A nil DefaultPlanID clears the default plan.
// The voucher counter only moves up here: a form opened before a batch was made must not roll it back.
func (s *Store) UpdateSettings(ctx context.Context, st domain.Settings, now time.Time) error {
	var def any
	if st.DefaultPlanID != nil {
		def = *st.DefaultPlanID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE settings SET business_name=?, timezone=?, radius_shared_secret=?, coa_port=?,
		accounting_interval_seconds=?, default_plan_id=?, batch_username_prefix=?,
		batch_counter=max(batch_counter, ?), batch_max_count=?, voucher_password_length=?, voucher_help=?,
		session_retention_days=?, admin_addr=?, radius_auth_addr=?, radius_acct_addr=?,
		log_level=?, log_format=?, updated_at=? WHERE id=1`,
		st.BusinessName, st.Timezone, st.RadiusSharedSecret, st.CoaPort, st.AccountingIntervalSeconds, def,
		st.BatchUsernamePrefix, st.BatchCounter, st.BatchMaxCount,
		st.VoucherPasswordLength, st.VoucherHelp, st.SessionRetentionDays,
		st.AdminAddr, st.RadiusAuthAddr, st.RadiusAcctAddr, st.LogLevel, st.LogFormat, now.Unix())
	if sqliteCode(err) == sqliteConstraintFK { // the default plan was deleted after the service checked it
		return domain.ErrPlanGone
	}
	return err
}
