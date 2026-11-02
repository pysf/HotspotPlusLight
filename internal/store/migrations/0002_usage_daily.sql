-- Phase 9 (D12): daily usage history. One row per member and calendar day (YYYY-MM-DD in
-- settings.timezone when the accounting packet arrived). Fed by the same deltas as member.used_*.
-- Renewals do not touch it; deleting the member removes its rows.
CREATE TABLE usage_daily (
  member_id INTEGER NOT NULL REFERENCES member(id) ON DELETE CASCADE,
  day TEXT NOT NULL,
  down INTEGER NOT NULL DEFAULT 0,
  up INTEGER NOT NULL DEFAULT 0,
  seconds INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (member_id, day)) WITHOUT ROWID;
CREATE INDEX usage_daily_day ON usage_daily(day);
