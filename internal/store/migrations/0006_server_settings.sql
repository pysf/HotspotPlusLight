-- 0006_server_settings: listen addresses and log settings move to the settings table.
-- Env vars only seed these values on the first start; the dashboard wins after that. Changes take effect after restart.
ALTER TABLE settings ADD COLUMN admin_addr       TEXT NOT NULL DEFAULT ':8081';
ALTER TABLE settings ADD COLUMN radius_auth_addr TEXT NOT NULL DEFAULT ':1812';
ALTER TABLE settings ADD COLUMN radius_acct_addr TEXT NOT NULL DEFAULT ':1813';
ALTER TABLE settings ADD COLUMN log_level        TEXT NOT NULL DEFAULT 'info';
ALTER TABLE settings ADD COLUMN log_format       TEXT NOT NULL DEFAULT 'text';
