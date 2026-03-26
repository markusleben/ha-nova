# Client Registry Bundle Parity Plan

Date: 2026-03-17

1. Copy `clients/` into install bundles.
2. Require `clients/registry.json` in staged bundle validation.
3. Remove panic-based registry loading from user-facing runtime paths.
4. Add/adjust tests for:
   - graceful runtime failure
   - bundle validation
   - release bundle contents
5. Run `go test ./...` and `npm run verify`.
