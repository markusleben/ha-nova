# Final Review Hardening Plan

Date: 2026-03-15

1. Fix CLI/setup correctness
   - normalize `--host` / `--ha-url` in non-interactive setup
   - make `back` work even when `--relay-token` was supplied
   - treat reachable `homeassistant.local` as confirmed discovery

2. Fix install/uninstall/dev-state correctness
   - gate Unix PATH removal on `state.PathManaged`
   - make Claude install recover from stale `installed_plugins.json`
   - extend Windows cleanup for Claude plugin artifacts
   - validate `all` in Windows desktop setup
   - remove broken fake `.exe` fallback in local skill installer

3. Fix docs/contracts/harness truthfulness
   - bind mock reported version to served bundle version, not package defaults
   - verify all advertised harness assets before declaring ready
   - narrow client docs/contracts to the validated Windows matrix
   - remove obsolete `git pull` wording from legacy helper text

4. Verify
   - targeted tests for each changed area
   - `cd cli && go test ./...`
   - `npm run verify`

5. Review
   - multi-agent review on the fix delta
