# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## New Features

- Reviews and writes load less mandatory context: the stale vendored
  templating snapshot is retired in favour of the live render loop and the
  official templating page on demand; output rules are read once per
  session.

## What To Watch

- **Malformed Home Assistant replies now fail loud.** A core REST reply
  whose body is not valid JSON or not valid UTF-8 is reported as an upstream
  error (`502 / UPSTREAM_HTTP_ERROR`) instead of a successful empty result.
- **`RELAY_PORT` must be a plain integer.** Values such as `8791oops` or
  `1.5` are rejected at startup instead of silently starting on a different
  port.
- **Private App files must belong to the App and be owner-only.** A
  restored `/data` whose files are readable by other users is repaired to
  owner-only on first read; files owned by another user stop the App with a
  message naming the `chown` fix — never with a registry reset offer.

## Bug Fixes

- Home Assistant 2026.9 child devices (`parent_device_id`, reduced rows,
  inherited area) are modeled once in the shared membership-resolution
  contract and used by health, capability answers, and fallback resolution.
- Update install/skip/clear-skipped are administrator-only since HA 2026.9:
  a standalone Relay with a non-administrator token gets a clear diagnosis
  instead of an opaque failure.

- A Home Assistant WebSocket peer that accepts the connection and never
  finishes authentication no longer wedges every later Relay call: the
  handshake has a deadline, and an abandoned connection is closed instead of
  reconnecting next to its replacement.
- Pairing, management-page actions, and owner verification log the real
  cause of a persistence or lookup failure for the operator; the replies a
  client sees stay unchanged.
- A malformed owner lookup answers "verification unavailable, retry"
  instead of a false "owner access required".
- Windows: the updater no longer leaves `ha-nova-updater-*.exe` in `%TEMP%`,
  a failed updater launch cleans up its staged download, and the uninstall
  helper removes itself on every exit.
- Configuration snapshots: concurrent saves can no longer exceed the store's
  hard file and size caps; a snapshot removed while the store is being
  scanned no longer fails the scan.
- The management page and health endpoint keep answering when the snapshot
  store is unreadable, and the operator log says why.

## Internal

- Every GitHub Actions step is pinned to an immutable commit SHA, and CodeQL
  scans the Go CLI fail-closed.
- The per-pull-request release gating is replaced by release-time checks:
  signed Cloud bundle provenance, three-OS install smokes, and a per-release
  Cloud checklist.
- Route-level context ratchets for the review and write skills (#521).
