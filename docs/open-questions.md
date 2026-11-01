# Open questions for the owner (running list)

Work continues on the stated default until you answer. Newest last.

| # | Phase | Question | Working default |
|---|---|---|---|
| 1 | 3 | Member passwords may not contain control characters (tab etc.); batch/generated passwords are digits. OK? | Forbidden (BR-14a) so the wrong-secret heuristic can never drop a real password |
| 2 | 3 | Keep the `1k` floor for speeds that round to 0 kbps (legacy sent `0k`, which RouterOS reads as unlimited)? | Keep `1k` |
| 3 | 3 | Build the bad-secret counters on the router link status panel (PLAN §8.2)? Today only a WARN log line exists | Not built |
| 4 | 3 | The concurrent-session check is not atomic with accept (two simultaneous logins at `max_sessions=1` can both pass) — legacy-faithful, `MONITOR:` comment | Left as legacy |
