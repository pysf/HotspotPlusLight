package store

import (
	"context"
	"database/sql"
	"time"

	"hotspotpluslight/internal/domain"
)

// GetSettings returns the single settings row.
func (s *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	var st domain.Settings
	var def sql.NullInt64
	err := s.r.QueryRowContext(ctx, `SELECT business_name, timezone, radius_shared_secret, coa_port, accounting_interval_seconds,
		default_plan_id, batch_username_prefix, batch_counter, batch_max_count,
		voucher_password_length, voucher_help, session_retention_days,
		admin_addr, radius_auth_addr, radius_acct_addr, log_level, log_format FROM settings WHERE id=1`).
		Scan(&st.BusinessName, &st.Timezone, &st.RadiusSharedSecret, &st.CoaPort, &st.AccountingIntervalSeconds, &def,
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
func (s *Store) UpdateSettings(ctx context.Context, st domain.Settings, now time.Time) error {
	var def any
	if st.DefaultPlanID != nil {
		def = *st.DefaultPlanID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE settings SET business_name=?, timezone=?, radius_shared_secret=?, coa_port=?,
		accounting_interval_seconds=?, default_plan_id=?, batch_username_prefix=?,
		batch_counter=?, batch_max_count=?, voucher_password_length=?, voucher_help=?,
		session_retention_days=?, admin_addr=?, radius_auth_addr=?, radius_acct_addr=?,
		log_level=?, log_format=?, updated_at=? WHERE id=1`,
		st.BusinessName, st.Timezone, st.RadiusSharedSecret, st.CoaPort, st.AccountingIntervalSeconds, def,
		st.BatchUsernamePrefix, st.BatchCounter, st.BatchMaxCount,
		st.VoucherPasswordLength, st.VoucherHelp, st.SessionRetentionDays,
		st.AdminAddr, st.RadiusAuthAddr, st.RadiusAcctAddr, st.LogLevel, st.LogFormat, now.Unix())
	return err
}
