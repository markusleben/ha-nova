# 2026-03-16 mDNS Browse Leading Space Fix Plan

1. Add a failing regression test for indented `dns-sd -B` output.
2. Apply the smallest parser fix in `cli/setup_discovery.go`.
3. Run focused tests, full CLI tests, and `npm run verify`.
4. Request parallel reviews on the delta.
