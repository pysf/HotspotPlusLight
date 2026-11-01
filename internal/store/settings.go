package store

import (
	"context"
	"database/sql"
	"time"

	"hotspotpluslight/internal/domain"
)

func (s *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	var st domain.Settings
	var def sql.NullInt64
	var mac int
	err := s.db.QueryRowContext(ctx, `SELECT site_title, timezone, radius_shared_secret, coa_port, accounting_interval_seconds,
		default_plan_id, mac_auto_login, after_login_url, batch_username_prefix, batch_counter, batch_max_count,
		voucher_password_length, voucher_help, public_base_url, session_retention_days FROM settings WHERE id=1`).
		Scan(&st.SiteTitle, &st.Timezone, &st.RadiusSharedSecret, &st.CoaPort, &st.AccountingIntervalSeconds, &def, &mac,
			&st.AfterLoginURL, &st.BatchUsernamePrefix, &st.BatchCounter, &st.BatchMaxCount, &st.VoucherPasswordLength,
			&st.VoucherHelp, &st.PublicBaseURL, &st.SessionRetentionDays)
	if def.Valid {
		v := def.Int64
		st.DefaultPlanID = &v
	}
	st.MacAutoLogin = mac != 0
	return st, err
}

func (s *Store) UpdateSettings(ctx context.Context, st domain.Settings, now time.Time) error {
	var def any // NULL when no default plan
	if st.DefaultPlanID != nil {
		def = *st.DefaultPlanID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE settings SET site_title=?, timezone=?, radius_shared_secret=?, coa_port=?,
		accounting_interval_seconds=?, default_plan_id=?, mac_auto_login=?, after_login_url=?, batch_username_prefix=?,
		batch_counter=?, batch_max_count=?, voucher_password_length=?, voucher_help=?, public_base_url=?,
		session_retention_days=?, updated_at=? WHERE id=1`,
		st.SiteTitle, st.Timezone, st.RadiusSharedSecret, st.CoaPort, st.AccountingIntervalSeconds, def,
		boolInt(st.MacAutoLogin), st.AfterLoginURL, st.BatchUsernamePrefix, st.BatchCounter, st.BatchMaxCount,
		st.VoucherPasswordLength, st.VoucherHelp, st.PublicBaseURL, st.SessionRetentionDays, now.Unix())
	return err
}
