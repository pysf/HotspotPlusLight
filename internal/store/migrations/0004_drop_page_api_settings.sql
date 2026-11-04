-- Phase 12a: the page API and MAC auto-login are gone, so their three settings go too.
ALTER TABLE settings DROP COLUMN mac_auto_login;
ALTER TABLE settings DROP COLUMN after_login_url;
ALTER TABLE settings DROP COLUMN public_base_url;
