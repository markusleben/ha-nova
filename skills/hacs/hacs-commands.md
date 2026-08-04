# HACS Command Map (pinned)

Pinned against HACS 2.x (verified 2.0.5, `custom_components/hacs/websocket/`).
All commands are admin-only WS commands and go through `ha-nova relay ws`.
This map is the ONLY set of `hacs/*` commands the skill may send. Never guess
schemas; never edit `.storage`; never SSH — every operation reaches Home
Assistant exclusively through the Relay, and anything outside this map stops
with the HACS-UI pointer.

## Capability gate — read `hacs/info` first

`{"type":"hacs/info"}` → object with `version`, `categories` (active list),
`disabled_reason`, `startup`, `has_pending_tasks`, `lovelace_mode`, `stage`.

Proceed only when ALL hold; otherwise fail closed with the HACS-UI pointer on
an unrecognized or unhealthy install:

- `version` parses and is a recognized 2.x line, and the payload has NO
  `experimental` key (1.x lines sent it; 2.0 removed it — its presence
  marks a pre-2.0 install whose command surface this map does not cover).
- `disabled_reason` is null (`rate_limit`, `removed`, `invalid_token`,
  `constrains`, `load_hacs`, `restore` all mean HACS itself is not
  operational).
- For mutations: `startup` is false and `has_pending_tasks` is false —
  otherwise data is incomplete; reads may proceed with that caveat stated.

`lovelace_mode` decides frontend verification: `storage` → the Lovelace
resource registration is checkable; any other mode (`yaml`, `auto`,
`auto-gen`) → resources are user-managed.

1.x background (fail-closed rationale only — never call these): the pre-1.26
API used `hacs/config`/`hacs/status`, `hacs/repositories`, and action-based
`hacs/repository`/`hacs/repository/data` commands. An unrecognized schema is
therefore a real possibility on old installs, and the answer is the HACS UI,
not improvisation.

## Repository identity (critical)

Per-repository commands take the HACS repository ID (stringified GitHub
numeric id from `list`/`info`) in a field named `repository` or
`repository_id`. TWO exceptions take other values: `hacs/repositories/add`
(`repository` = GitHub URL or `owner/repo`) and `hacs/critical/acknowledge`
(`repository` = `full_name`). Resolve IDs fresh from `hacs/repositories/list`
or `hacs/repository/info` in the same session before every mutation — IDs are
never guessed, reused across sessions, or derived from names.

## Reads

- `{"type":"hacs/repositories/list","categories":[...]}` (categories
  optional; default all active) → rows with `id`, `full_name`, `name`,
  `category`, `custom`, `installed`, `installed_version`,
  `available_version`, `pending_upgrade`, `config_flow`, `domain`,
  `can_download`, `status`, `homeassistant` (min HA version), `stars`,
  `downloads`, `last_updated`.
- `{"type":"hacs/repository/info","repository_id":"<id>"}` → the row plus
  `releases` (published tags), `selected_tag`, `beta`, `default_branch`,
  `version_or_commit`, raw README/info markdown. Side effects: forces a GitHub
  refresh on first call and clears the `new` flag. Clean
  `repository_not_found` error on a bad ID.
- `{"type":"hacs/repository/releases","repository_id":"<id>"}` →
  `[{name, tag, published_at, prerelease}]` (live GitHub fetch; error code
  `unknown` on failure).
- `{"type":"hacs/repository/release_notes","repository":"<id>"}` →
  `[{name, body, tag}]` for releases newer than the installed version
  (ALL cached releases when nothing is installed).
- `{"type":"hacs/repositories/removed"}` → repositories removed from the
  default store (surface these as warnings when installed).
- `{"type":"hacs/critical/list"}` → critical repositories
  (`repository` = full_name, `reason`, `link`, `acknowledged`).

## Mutations

- `{"type":"hacs/repositories/add","repository":"<url-or-owner/repo>","category":"<category>"}` —
  registers a custom repository; does NOT download. The socket cannot
  report add failures reliably: parseable requests return `{}` even on
  failure, an unparseable URL returns NOTHING (a Relay timeout), and an
  invalid category fails log-only. The canonical re-list for the expected
  `full_name` is the ONLY complete verification; an add that does not
  appear in the re-list failed.
- `{"type":"hacs/repositories/remove","repository":"<id>"}` — unregisters a
  custom repository; downloaded files stay (file removal is
  `hacs/repository/remove`).
- `{"type":"hacs/repository/download","repository":"<id>","version":"<tag>"}` —
  install/redownload; `version` optional (default latest). Errors with code
  `error` on a HACS failure.
- `{"type":"hacs/repository/remove","repository":"<id>"}` — uninstall:
  deletes the downloaded files.
- `{"type":"hacs/repository/version","repository":"<id>","version":"<tag>"}` —
  sets `selected_tag`; does NOT download. Ordering is critical: a `download`
  WITHOUT `version` resolves to the LATEST release, and EVERY download
  clears `selected_tag` when it finishes — so installing a specific version
  means passing `version` DIRECTLY to `download`, and a persistent pin is
  set with this command AFTER the verified download. HACS still reports a
  tag-pinned repository as `pending_upgrade`; a pin never suppresses update
  signaling — disclose that.
- `{"type":"hacs/repository/beta","repository":"<id>","show_beta":true}` —
  toggles prerelease visibility; re-fetches releases.
- `{"type":"hacs/repository/refresh","repository":"<id>"}` — forced GitHub
  data refresh (no file changes); also refreshes the `update.*` entities.
- `{"type":"hacs/repository/ignore","repository":"<id>"}` — hides the
  repository from HACS listings (visibility flag, no file changes); clean
  `repository_not_found` on a bad ID. Only on explicit request.
- `{"type":"hacs/critical/acknowledge","repository":"<full_name>"}` —
  acknowledges a critical-repository notice (takes `full_name`, not the
  ID). Only on explicit request, after showing the critical reason.
- `{"type":"hacs/repositories/clear_new", ...}` and
  `{"type":"hacs/repository/state", ...}` are UI housekeeping — this skill
  does not send them.

## Error asymmetry (treat as contract)

Only `repository/info` and `repository/ignore` return a clean
`repository_not_found`. `download` errors with code `error`; `releases` with
`unknown`. Most other ID-taking commands fail with a generic `unknown_error`
on an unknown ID — which is why IDs are always re-resolved immediately before
use, and why any generic error triggers the reconcile loop instead of a
retry.

## Categories and restart semantics

Categories (2.x): `integration`, `plugin` (frontend), `theme`, `template`,
`python_script`, `appdaemon` — always read the ACTIVE set from
`hacs/info.categories`; never assume.

Integration downloads generally leave the new code INSTALLED but not ACTIVE:
every upgrade — and the first install of a non-config-flow integration —
requires a Home Assistant restart before the running component changes (the
one exception is the first install of a `config_flow` integration, which HACS
live-loads). Uninstalling a non-config-flow integration ALSO sets pending
restart — the files are gone but the loaded component keeps RUNNING until
Home Assistant restarts. Pending state surfaces as
`status: "pending-restart"` in `list`/`info` and as a fixable `hacs` repair
issue (`restart_required_*`).
`plugin`/`theme`/`template`/`python_script`/`appdaemon` never set
pending-restart; frontend packages need a browser/frontend reload instead.
