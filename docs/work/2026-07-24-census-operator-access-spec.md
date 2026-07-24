# Census Operator Access

Status: staged decision
Date: 2026-07-24

## Problem

The GitHub release-download estimator predates the opt-in census and cannot
separate users from CI, retries, updates, or maintainer installs. The census
now provides the intended directional version and platform signal directly.

The current `GET /stats` endpoint is public by deliberate v0.21 design. The
maintainer now prefers private operator-only visibility initially.

## Phase 1: obsolete estimator cleanup

- Remove `scripts/dev/release-download-stats.sh`.
- Preserve the existing ignored local own-activity ledger. It is no longer
  consumed, but remains user-owned local data unless separately removed.
- Keep the deployed census and its current access contract unchanged.

## Proposed Phase 2: private statistics

- Keep unauthenticated `POST /ping` public so opted-in clients continue to
  report without credentials.
- Protect only `GET /stats` with Cloudflare Access and allow the maintainer
  identity.
- Provide one documented browser path and one maintainer CLI command that
  renders the aggregate JSON as a compact report.
- Update every public claim from public statistics to maintainer-visible
  aggregate statistics before changing production access.
- Update deployment verification to authenticate without placing credentials
  in Git or command output.
- Preserve aggregate-only storage, the no-source-IP-read invariant, and the
  existing payload contract.

## Verification

- No active source consumes the removed estimator.
- `POST /ping` remains unauthenticated and contract-tested.
- Unauthenticated `GET /stats` is rejected after Phase 2.
- Authenticated browser and CLI access show the same aggregate response.
- Release deployment verification proves the reviewed Worker SHA and version
  through the authenticated path.
