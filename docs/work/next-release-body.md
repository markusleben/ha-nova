# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## New Features

- Reviews and writes load less mandatory context: the stale vendored
  templating snapshot is retired in favour of the live render loop and the
  official templating page on demand; output rules are read once per
  session.
- New read-only commands `ha-nova find`, `ha-nova get`, and `ha-nova history`
  look up entities (case- and accent-insensitive, aliases included), read a
  state or stored automation/script/scene config, and summarize recent
  history in one call each. They can never change anything in Home
  Assistant, and they report available updates alongside the answer.
- New commands `ha-nova call` and `ha-nova apply` control devices with the
  safety checks built in: unambiguous everyday actions run immediately and
  are verified afterwards; everything else shows a preview first; unlocking a
  door, opening a gate or garage door, and disarming the alarm need a
  confirmation code. A call is sent exactly once and never retried, and a
  confirmed plan is re-checked first — if anything changed since the
  preview, nothing is sent and a fresh preview comes back.
- Automations and scripts can be created, updated, deleted, and reverted
  through `ha-nova plan write|delete|revert` and `ha-nova apply`: every
  change shows its diff first, is saved exactly once, reloaded, and read back
  to verify it; deletes need a confirmation code and take a config snapshot
  first, and the last update stays one step revertible.

- **Use HA NOVA from any MCP client.** `ha-nova mcp` serves the same
  find/get/history/trace/read, plan, call, and apply checks as tools to
  clients without a shell (Claude Desktop and others), with the skills as
  prompts; it reuses this computer's pairing. See the client integration
  reference for the one-line configuration.

## What To Watch

- **The safety promise has one explicit exception now.** Unambiguous
  everyday actions run immediately and are verified afterwards; everything
  else shows a preview first; locks, gates, alarm, and deletes need a code.
  The HA NOVA command decides which is which, never the AI — and it sends
  nothing when anything changed since a preview.
- **Automation and script deletes use one code per item.** The
  confirmation code for deleting an automation or script now comes from
  HA NOVA for that exact delete; a multi-item delete asks for each item's
  own code instead of one code for the whole list. Reverting an update that
  was changed in the Home Assistant UI since is refused instead of
  overwriting that edit.
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
- **Shared legacy access is being retired.** App installs that still use
  the old shared access from before per-device pairing keep working in this
  release and the next one; the release after that no longer accepts it.
  Re-pair each computer
  once with `ha-nova pair` (one six-digit code each), then revoke the old
  shared access on the NOVA page. The standalone container's shared token
  is retired on the same schedule and moves to the same one-time-code
  pairing over a pinned connection.
- **The Census is gone; HA NOVA collects nothing.** The opt-in Census was
  removed: the CLI never asks, never sends a report, and deletes its local
  Census state automatically on the first run. `ha-nova census …` no longer
  exists.

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
- Headless Linux: `ha-nova uninstall --purge` works on installs without an
  unlocked keyring (service mode, file-stored credentials) instead of
  stopping at "secure storage unavailable" and removing nothing.
- Codex CLI and OpenCode can use `ha-nova setup --service <client>` on
  headless machines and SSH sessions; and re-running `ha-nova setup` there
  keeps an existing service token file instead of demanding the keyring.
- fish users: the installer puts `~/.local/bin` on PATH through
  `~/.config/fish/conf.d/ha-nova.fish` (other unknown shells: `~/.profile`),
  and uninstall removes the PATH entry from every file an installer wrote.
- Uninstall wording: the summary names the relay token file when one is
  used, and the server-side checklist covers standalone relay containers.

## Internal

- Every GitHub Actions step is pinned to an immutable commit SHA, and CodeQL
  scans the Go CLI fail-closed.
- The per-pull-request release gating is replaced by release-time checks:
  signed Cloud bundle provenance, three-OS install smokes, and a per-release
  Cloud checklist.
- Route-level context ratchets for the review and write skills (#521).

## Release-prep README edits

- MCP adapter: reframe "Why not an MCP server?" — HA NOVA stays skills-first
  (one readable skill on demand, no default tool catalog), and now also ships
  a small local MCP server (`ha-nova mcp`) for clients without a shell that
  reuses this computer's pairing and runs the same checks; add "local MCP
  clients that launch a command (stdio)" to Supported Clients — no remote/HTTP
  MCP endpoint.
- Confirmation-code honesty: wherever the README promises codes ("You
  approve it", typed codes for locks/gates/alarm/deletes), add that a code
  only proves it was you when your client asks before the apply step runs —
  the AI sees the code it relays; HA NOVA tells it never to use one you did
  not type, and over MCP keep the client's approval prompt on for `ha_apply`
  (never "always allow").
- Batch deletes: README step 3 "A reviewed batch delete takes one code bound
  to the exact list of items" no longer holds for automations and scripts —
  each delete is its own plan with its own code.
- Typed verbs: the README promise "Every change is previewed and approved
  first" (tagline) and "device control get preview-and-verify" (feature
  note) change to the fast-path wording above — unambiguous everyday actions
  run immediately and are verified afterwards; everything else shows a
  preview first; locks, gates, alarm, and deletes need a code.
- Census removal: delete the README "Census off by default" Safe-by-Design
  bullet (it also links the removed `docs/reference/census.md`), and change
  the `ha-nova uninstall --purge` row to say it retains small opaque local
  markers that block stale processes from restoring the install.
- Headless Linux (README "Linux — proven end-to-end, including headless
  setups"): add that a standalone relay (relay token, no device pairing) on a
  machine without a desktop keyring uses `ha-nova setup --service <client>`
  (Codex CLI, OpenCode, Hermes Agent).
