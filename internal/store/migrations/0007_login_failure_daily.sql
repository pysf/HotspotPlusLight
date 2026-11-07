-- 0007_login_failure_daily: a daily count of failed logins by reason (BR-27).
-- It stores only counts: no usernames and no addresses.
CREATE TABLE login_failure_daily (
  day TEXT NOT NULL,     -- 'YYYY-MM-DD' in settings.timezone at receive time, like usage_daily
  reason TEXT NOT NULL,  -- a reject code, for example INVALID_CREDENTIALS
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, reason)) WITHOUT ROWID;
