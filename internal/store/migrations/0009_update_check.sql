-- 0009_update_check: the "Check for updates" setting and the result of the last check.
ALTER TABLE settings ADD COLUMN check_updates     INTEGER NOT NULL DEFAULT 1 CHECK (check_updates IN (0, 1));
ALTER TABLE settings ADD COLUMN update_latest     TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN update_url        TEXT    NOT NULL DEFAULT '';
ALTER TABLE settings ADD COLUMN update_checked_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE settings ADD COLUMN update_error      TEXT    NOT NULL DEFAULT '';
