-- Phase 14: "site title" becomes "business name" and is empty by default.
-- SQLite cannot change a column default, so the table is rebuilt. An old default value 'HotspotPlus' is cleared.
CREATE TABLE settings_new (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  key_check TEXT NOT NULL DEFAULT '',
  business_name TEXT NOT NULL DEFAULT '',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  radius_shared_secret TEXT NOT NULL,
  coa_port INTEGER NOT NULL DEFAULT 3799,
  accounting_interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (accounting_interval_seconds >= 10),
  default_plan_id INTEGER REFERENCES plan(id) ON DELETE SET NULL,
  batch_username_prefix TEXT NOT NULL DEFAULT 'wifi',
  batch_counter INTEGER NOT NULL DEFAULT 1360,
  batch_max_count INTEGER NOT NULL DEFAULT 30,
  voucher_password_length INTEGER NOT NULL DEFAULT 6,
  voucher_help TEXT NOT NULL DEFAULT '',
  session_retention_days INTEGER NOT NULL DEFAULT 90,
  updated_at INTEGER NOT NULL);

INSERT INTO settings_new(id, key_check, business_name, timezone, radius_shared_secret, coa_port, accounting_interval_seconds,
    default_plan_id, batch_username_prefix, batch_counter, batch_max_count, voucher_password_length, voucher_help,
    session_retention_days, updated_at)
  SELECT id, key_check, CASE WHEN site_title = 'HotspotPlus' THEN '' ELSE site_title END, timezone, radius_shared_secret, coa_port,
    accounting_interval_seconds, default_plan_id, batch_username_prefix, batch_counter, batch_max_count, voucher_password_length,
    voucher_help, session_retention_days, updated_at
  FROM settings;

DROP TABLE settings;
ALTER TABLE settings_new RENAME TO settings;
