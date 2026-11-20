# 2026-03-17 — JQ Escape Hardening Plan

1. Add a focused jq normalization retry for the known bare `\.` parse failure.
2. Add a Go regression test that reproduces the failing filter.
3. Rewrite helper-domain skill examples to avoid regex escaping entirely.
4. Add a skill contract test so regex-dot helper examples do not return.
5. Run targeted tests, full Go tests, and `npm run verify`.
