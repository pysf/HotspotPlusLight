-- Phase 11b: a fresh install starts with admin/admin and must set a new password at first login.
-- Existing installs keep 0, so they are not forced.
ALTER TABLE admin ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
