# Role loss takes effect at once (MAR-170, MAR-181)

Status: 0.36, shaped for KISS by openai/gpt-6.2-sol (high). Both fixes close a
window in which a person who lost a Home Assistant role keeps acting with it.
Both fail closed when the role can't be read.

## 1. A computer an administrator connected (MAR-170, App)

Since 0.31 any active administrator can make the six-digit code on the NOVA
page (#1145), and the computer it connects kept its access after that person
lost the role.

- **Who made the code.** `generateCode(madeBy?)`: the NOVA page passes the
  acting user's id when that user is an administrator but not the owner. The
  open code carries it, and the device record it creates stores it as
  `pairedBy` (activation keeps it). The owner's codes, sign-in pairing (owner
  only) and the App's own bridge and gate devices store nothing: Home
  Assistant doesn't let the owner be deactivated or deleted, and these devices
  keep working while Home Assistant restarts.
- **Every request checks it.** The device listener (pinned TLS) and the Home
  Assistant Link listener resolve the principal as before; when the record has
  `pairedBy`, they read `config/auth/list` fresh (no cache) and accept only if
  that user is still an active owner or administrator (`isAdmin`), then
  resolve the credential again, so a revoke during the read still wins
  (`checkPairedBy`). The check runs in the listener's `authorize` hook, so it
  runs again before dispatch and before a held request is sent after its
  approval (`reauthorize`).
  - Not an administrator any more, deactivated, or deleted: `401
    UNAUTHORIZED` ("the administrator who connected this computer no longer
    manages this Home Assistant; connect it again with a new code"). The CLI
    already reads a 401 as "pair this computer again".
  - The users can't be read: `503 ROLE_UNKNOWN` (after an approval wait,
    `reauthorize` reports it as a refusal), nothing runs.
  - The role comes back: the computer works again. Nothing is deleted, so an
    accidental demotion costs nothing; revoking stays on the page.
- **Rollback fails closed.** A record with `pairedBy` is written under its own
  key, `roleCheckedDevices`, never under `devices`; this App reads both (a
  record in the wrong list is a damaged registry). The previous App reads only
  `devices`, so after a rollback those computers are unknown (401) and its
  next write drops them; every other computer keeps working. (Shaping replaced
  a registry version bump, which would have shut out every computer.)
- **Activation** (`/auth/device/activate`, Link bind and activate) stays
  self-authenticated: it needs the computer's own new credential, can only
  replace that same computer's older entry, and the computer it activates is
  checked on every use.
- **Computers connected before 0.36** carry no `pairedBy`: nobody recorded who
  made their code. They keep their access until revoked (known limit, release
  note: revoke and reconnect them to get the check).

## 2. A chat app's change reads the role under the lock (MAR-181, CLI)

Today `ha_call` and `ha_apply` from a chat app (`ha-nova serve`) read the
user's Home Assistant role when the request arrives (`chatGate`), then wait up
to 30 s for `mcpVerbLock`; under the lock only `chat-apps.json` is read again
(`reread`). A user demoted meanwhile still runs the change at the old level.

- One place: `runLocked` (`chatLocked`), under the lock and before the call's
  policy is published, reads the user's policy with Home Assistant's users
  read now and then the owner's settings (`reread`), and runs the switch and
  level checks again (`chatRefusal`, `chatLevelRefusal`, which also covers
  argument levels such as `no_snapshot`). The verb's own checks
  (`chatCallFailure`, `chatPlanFailure`) use that policy; an
  administrator-only plan needs Manage everything, which only an
  administrator's role gives (`roleCap`). The policy goes on the call, not on
  the server, because the job's `awaitJob` reads the server's.
- It runs for `ha_call`, `ha_apply` and config plans (`ha_plan` of any kind but a call, whose preview shows live config: found by the sibling sweep), on the ordinary tool path, `applyJob` and `phoneJob`'s first
  phase. `phoneJob` after the tap and the Relay-container ask (`askOutside`,
  after the OK) already read again under the lock; they pass no arguments and
  skip it.
- Refusals are the existing ones: another users read still running → `BUSY`
  (nothing ran, the plan stays valid), users unreadable → `ACCESS_UNKNOWN`, no
  longer allowed → `CHAT_APPS_REFUSED`, level too low → `CHAT_LEVEL`,
  read only → `READ_ONLY`; nothing is sent.
- Speed: the fresh read moves from arrival to under the lock for `ha_call` and
  `ha_apply` (arrival uses the users as read within the last minute, as every
  other tool does), so a change still costs one users read. The arrival admin
  check for config plans (`chatAdminRefusal`) stays: it answers
  `ADMIN_ONLY` early with its own words.

## Tests

- Relay: an administrator's code stores `pairedBy`, the owner's doesn't and
  reads no users; a request from that computer is refused after demotion,
  deactivation and deletion, works again after re-promotion, and gets 503 when
  the users can't be read, on the device and the Link listener; the registry
  keeps such computers under `roleCheckedDevices` only, and fails closed on a
  record in the wrong list.
- CLI: `ha_call` and `ha_apply` with the user demoted while the request waits
  for the lock: `CHAT_LEVEL`, nothing sent (fails on the code before this
  change).
