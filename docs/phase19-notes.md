# Phase 19 notes: dashboard docs and clean-up (2026-11-07)

Built: the site page `docs/dashboard/` (what each tile, list and chart means, and how failed logins are counted and kept), linked in the docs sidebar of every page (between First login and Router setup), in the pager, in `sitemap.xml` and in `search-index.js`. The README has a Dashboard section, the package map rows for `internal/dashboard` and `internal/linkstatus`, and two decision rows. First login, Router setup and Troubleshooting now say the router status is on the Dashboard. `docs/open-questions.md` row 43 is closed. CLAUDE.md status says phases 0-19 are done in code.

## Tests
`e2e/dashboard_docs_test.go` E19.01-E19.03 (written, not run by the agent). `TestE13_14_DocsLayout` now expects 12 docs pages (it was 11); its other checks are unchanged.

## Not verified / known
- No e2e test was run by the agent.
- Some other search index entries are older than their pages (installation, configuration, running-as-a-service, backup). Only the entries touched here were rebuilt.
