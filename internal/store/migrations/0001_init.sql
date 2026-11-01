-- 0001_init: PLAN.md section 6. Forward-only; never edit once released (checksum-verified).
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL,
  checksum TEXT NOT NULL);                     -- sha256 of the migration file (Q38)

CREATE TABLE admin (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL,
  password_hash TEXT NOT NULL,                 -- bcrypt
  updated_at INTEGER NOT NULL);

CREATE TABLE admin_session (
  token_hash TEXT PRIMARY KEY,                 -- sha256 of the random 32-byte id sent in the cookie
  csrf_token TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL);                -- sliding: each request sets now + 3600

CREATE TABLE plan (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  duration_days INTEGER NOT NULL CHECK (duration_days > 0),
  speed_value REAL NOT NULL CHECK (speed_value >= 0),          -- 0 = no rate limit
  speed_unit  TEXT NOT NULL CHECK (speed_unit IN ('Kbps','Mbps','Gbps')),
  bulk_value  REAL NOT NULL DEFAULT 0 CHECK (bulk_value >= 0), -- 0 = unlimited
  bulk_unit   TEXT NOT NULL DEFAULT 'MB' CHECK (bulk_unit IN ('KB','MB','GB')),
  time_limit_minutes INTEGER NOT NULL DEFAULT 0 CHECK (time_limit_minutes >= 0), -- 0 = unlimited
  max_sessions INTEGER NOT NULL DEFAULT 1 CHECK (max_sessions >= 1),    -- simultaneous devices, per plan (Q27)
  auto_resubscribe INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);

CREATE TABLE settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  key_check TEXT NOT NULL DEFAULT '', -- AES-GCM encryption of a fixed test string, written at first boot; verified at every start
  site_title TEXT NOT NULL DEFAULT 'HotspotPlus',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  radius_shared_secret TEXT NOT NULL,
  coa_port INTEGER NOT NULL DEFAULT 3799,
  accounting_interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (accounting_interval_seconds >= 10),
  default_plan_id INTEGER REFERENCES plan(id) ON DELETE SET NULL,
  mac_auto_login INTEGER NOT NULL DEFAULT 0,
  after_login_url TEXT NOT NULL DEFAULT '',
  batch_username_prefix TEXT NOT NULL DEFAULT 'wifi',
  batch_counter INTEGER NOT NULL DEFAULT 1360,
  batch_max_count INTEGER NOT NULL DEFAULT 30,
  voucher_password_length INTEGER NOT NULL DEFAULT 6,
  voucher_help TEXT NOT NULL DEFAULT '',            -- English default text, admin-editable (O21)
  public_base_url TEXT NOT NULL DEFAULT '',         -- how clients reach this server; baked into the router page pack (O21)
  session_retention_days INTEGER NOT NULL DEFAULT 90,  -- 0 = keep forever (O18)
  updated_at INTEGER NOT NULL);

CREATE TABLE batch (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  label TEXT NOT NULL DEFAULT '',
  plan_id INTEGER NOT NULL REFERENCES plan(id) ON DELETE RESTRICT,
  count INTEGER NOT NULL,
  duration_days INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL);

CREATE TABLE member (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_enc BLOB NOT NULL,                       -- AES-256-GCM: nonce || ciphertext
  active INTEGER NOT NULL DEFAULT 1,
  first_name TEXT NOT NULL DEFAULT '',
  last_name  TEXT NOT NULL DEFAULT '',
  mobile TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  national_code TEXT NOT NULL DEFAULT '',          -- free text, no validation (legacy had none)
  mac TEXT NOT NULL DEFAULT '',
  plan_id INTEGER NOT NULL REFERENCES plan(id) ON DELETE RESTRICT,
  subscription_date INTEGER NOT NULL,
  extra_bulk_bytes INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER,                               -- NULL = never (O7)
  batch_id INTEGER REFERENCES batch(id) ON DELETE SET NULL,
  used_download_bytes INTEGER NOT NULL DEFAULT 0,   -- current validity window (BR-11)
  used_upload_bytes   INTEGER NOT NULL DEFAULT 0,
  used_seconds        INTEGER NOT NULL DEFAULT 0,
  last_renewed_at INTEGER,                          -- NULL = never renewed (Q21)
  renewal_count INTEGER NOT NULL DEFAULT 0,         -- renewals since creation, automatic + manual (Q21)
  last_renewal_kind TEXT NOT NULL DEFAULT '' CHECK (last_renewal_kind IN ('','auto','manual')),
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX member_mac  ON member(mac) WHERE mac <> '';
CREATE INDEX member_plan ON member(plan_id);

CREATE TABLE session (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  member_id INTEGER NOT NULL REFERENCES member(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  acct_session_id TEXT NOT NULL,                    -- router's Acct-Session-Id
  nas_ip TEXT NOT NULL,                             -- packet source IP (used for kick)
  nas_identifier TEXT NOT NULL DEFAULT '',
  framed_ip TEXT NOT NULL DEFAULT '',
  mac TEXT NOT NULL DEFAULT '',
  status INTEGER NOT NULL CHECK (status IN (1,2,3)),-- 1 start, 2 stop, 3 interim
  started_at INTEGER NOT NULL,
  last_update_at INTEGER NOT NULL,
  ended_at INTEGER,
  terminate_cause TEXT NOT NULL DEFAULT '',
  download_bytes INTEGER NOT NULL DEFAULT 0,        -- last cumulative values from the router
  upload_bytes   INTEGER NOT NULL DEFAULT 0,
  session_seconds INTEGER NOT NULL DEFAULT 0);
CREATE INDEX session_open_member ON session(member_id) WHERE ended_at IS NULL;
CREATE INDEX session_open_key ON session(username, acct_session_id, nas_ip) WHERE ended_at IS NULL;
CREATE INDEX session_started ON session(started_at);
