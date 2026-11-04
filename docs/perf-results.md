# Capacity test results

Max online users on this machine: **30000** (last passing step). Run on 2026-11-04, each step 120 s after its warm-up.

Machine: OS darwin/arm64, CPU Apple M5, 10 cores, go1.26.0.

| Online users | Result | Start burst packets/s | Burst p50/p95/p99 (ms) | Steady packets/s | Auth p50/p95/p99 (ms) | Accounting p50/p95/p99 (ms) | Pages p50/p95/p99 (ms) | Lost | Server CPU % | Server RSS MB | DB MB |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 500 | pass | 17 | 3.2/6.5/8.2 | 9 | 2.6/4.0/6.9 | 1.6/3.1/3.6 | 3.2/5.7/7.0 | 0 | 2 | 32 | 4.4 |
| 1000 | pass | 34 | 2.3/3.2/3.5 | 17 | 1.9/2.7/2.8 | 1.4/2.1/2.5 | 2.8/6.5/6.7 | 0 | 4 | 33 | 4.6 |
| 2000 | pass | 68 | 2.3/3.1/3.6 | 34 | 1.5/2.6/2.9 | 1.2/1.9/2.4 | 3.1/7.2/8.2 | 0 | 7 | 34 | 5.0 |
| 5000 | pass | 170 | 1.8/3.1/3.5 | 86 | 1.0/2.0/2.5 | 1.3/1.9/2.4 | 5.2/12.1/21.3 | 0 | 17 | 36 | 6.2 |
| 10000 | pass | 340 | 1.6/2.3/3.1 | 172 | 0.9/1.7/2.1 | 1.8/3.1/3.6 | 7.6/13.5/16.4 | 0 | 30 | 40 | 8.1 |
| 20000 | pass | 513 | 1.2/3.1/4.4 | 343 | 0.9/1.7/2.1 | 2.2/3.9/4.7 | 14.2/24.2/25.0 | 0 | 50 | 41 | 12.2 |
| 30000 | pass | 517 | 1.3/37.7/114.3 | 513 | 0.7/1.5/2.0 | 2.4/4.3/5.3 | 18.7/25.2/42.2 | 0 | 61 | 44 | 16.1 |
| 40000 | FAIL | 519 | 1.4/889.0/1437.7 | 681 | 0.7/1.6/2.0 | 2.5/4.7/5.9 | 21.8/139.6/657.8 | 144 | 69 | 48 | 21.1 |

Step 40000 failed: 144 replies lost or later than 1 s; the load generator itself fell behind: dispatch lag 1.275s > 1s.

## What the test does

The test starts the real server as a child process with a fresh database. It plays a fake MikroTik router against it, over real UDP and HTTP.

For each step with N online users:
- N members are made in the database with one plan. Their passwords are encrypted like real ones.
- Warm-up: every user gets one Accounting Start, spread over the warm-up. The warm-up is a quarter of the step time (at least 3 s), and long enough that there are at most 500 Starts per second. It is a burst, so it is reported on its own ("Start burst").
- Steady part (120 s): every user sends one Interim Update each 60 seconds. So the server gets N/60 accounting packets per second.
- During the whole step, 10% of N extra members log in (Access-Request), spread over the step.
- 1% of the members (at least 2) are over their data quota. Half are over it before their Start (kicked at the Start). The other half cross it on their first Interim (so the Interim path is tested). The server must send a Disconnect for each. The fake router answers each Disconnect and then stops sending packets for that user.
- The admin Members page (/admin/members/) and Online sessions page (/admin/sessions/) are loaded every 3 seconds.
- The fake router uses 128 workers. Each has its own UDP sockets and one request in flight, so the 256 RADIUS identifiers per socket never run out. Open loop: every packet has a planned send time. Latency is measured from the planned time, so a slow load generator or a slow server both show up.

The steps are 500, 1000, 2000, 5000, 10000, 20000, 30000, 40000 and 50000 users. The test stops at the first step that fails.

## A step passes when

- no RADIUS reply is lost or later than 1 second (the router itself waits 5 seconds)
- every reply has the right code: Access-Accept for logins, Accounting-Response for accounting
- the load generator keeps up: dispatch lag (real send minus planned send) is at most 1 second, and the steady packets/s is at least 95% of the planned rate
- login (auth) p99 is below 500 ms (steady part)
- accounting p99 is below 200 ms (steady part)
- admin pages p95 is below 1 second
- there is no SQLITE_BUSY or "database is locked" in the server log
- the database shows the real work: the number of session rows ended by the server ("Server:...") equals the number of over-quota members, open sessions are about N minus those, and the sum of used download bytes matches what was sent and answered (within 0.5%)
- (extra check) at least 90% of the over-quota members got a Disconnect

The max online users is the last passing step. The test only measures: it does not fail the build unless you set HSP_PERF_MIN_USERS.

## Please note

- The real limit may be the MikroTik router itself, not this server. A small router may not handle thousands of Hotspot users and Interim updates.
- The fake router and the server run on the same machine and share its CPU. The server CPU and memory numbers are for the server process only.
- The CPU number is the share of one core (100 means one full core).
- Loopback has no network delay and no packet loss. A real network is slower.
- The test opens 2 UDP sockets per worker (256 here) and the server opens its own. If the test stops with "too many open files", raise the limit with `ulimit -n 4096` (Linux, macOS).
- If a step fails, the server log of that step is kept in bin/perf-last-fail.log.
- Run it again with `make perf` (full, writes this file and the README section) or `make perf-quick` (short, prints only). On Windows use `make perf-windows` and run bin/perf.test.exe with the flag -test.run TestCapacity -test.v -test.timeout 60m.
