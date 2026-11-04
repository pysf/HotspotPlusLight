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
		voucher_password_length, voucher_help, session_retention_days FROM settings WHERE id=1`).
		Scan(&st.BusinessName, &st.Timezone, &st.RadiusSharedSecret, &st.CoaPort, &st.AccountingIntervalSeconds, &def,
			&st.BatchUsernamePrefix, &st.BatchCounter, &st.BatchMaxCount, &st.VoucherPasswordLength,
			&st.VoucherHelp, &st.SessionRetentionDays)
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
		session_retention_days=?, updated_at=? WHERE id=1`,
		st.BusinessName, st.Timezone, st.RadiusSharedSecret, st.CoaPort, st.AccountingIntervalSeconds, def,
		st.BatchUsernamePrefix, st.BatchCounter, st.BatchMaxCount,
		st.VoucherPasswordLength, st.VoucherHelp, st.SessionRetentionDays, now.Unix())
	return err
}
