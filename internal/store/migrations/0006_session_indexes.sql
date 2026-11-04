-- Review A7: full indexes for the session table. The partial ones of 0001 cannot serve
-- the finished-row lookup, the member delete cascade, the batch "unused member" check
-- or the pruning of old rows.
CREATE INDEX session_member ON session(member_id);
CREATE INDEX session_key ON session(username, acct_session_id, nas_ip);
CREATE INDEX session_prune ON session(COALESCE(ended_at, last_update_at));
