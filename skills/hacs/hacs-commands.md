# HACS Command Map (pinned)

Pinned against HACS 2.x (verified 2.0.5, `custom_components/hacs/websocket/`).
All commands are admin-only WS commands and go through `ha-nova relay ws`.
This map is the ONLY set of `hacs/*` and `marketplace/*` commands the skill
may send. Never guess schemas; never edit `.storage`; never SSH — every
operation reaches Home Assistant exclusively through the Relay, and anything
outside this map stops with the HACS-UI pointer (Marketplace mode: its
Home Assistant steps).

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

### When `hacs/info` fails — classify before naming a cause

- WS error `unauthorized` (from `hacs/info` or `marketplace/info`) → the
  Relay upstream credential lacks HA admin (HACS and the Marketplace
  register every command admin-only) — remediation is switching that
  credential to an admin account, never an install hint.
- `unknown_command` (through the Relay: `UPSTREAM_WS_COMMAND_ERROR`, "HA
  rejected 'hacs/info': unknown_command") → send `{"type":"marketplace/info"}`:
  - a result → Marketplace mode (below). HACS is not missing; it is built in.
  - error `not_loaded` or `unknown_command` → read `{"type":"get_config"}`.
    `version` older than 2026.11 → `hacs/info` failing is ambiguous
    (missing, not loaded, or an unsupported line that does not register the
    pinned commands): read `{"type":"config_entries/get","domain":"hacs"}` —
    entry absent → install guidance (alternative: Home Assistant 2026.11+
    builds it in as the Marketplace); entry present but
    `not_loaded`/`setup_error`/`setup_retry`/`migration_error` →
    load/restart/repair remediation; entry present and loaded → unsupported
    schema, fail closed with the HACS-UI pointer.
    2026.11 or newer → the Marketplace exists but is not loaded, and HACS
    cannot be installed there — never install guidance. Take the first match:
    - `recovery_mode` true → Home Assistant runs in recovery mode, which
      loads no Marketplace: point to the Home Assistant logs.
    - `state` not `RUNNING` → Home Assistant is still starting (the
      Marketplace registers and loads late; the first 2026.11 start also
      takes over HACS): wait about 30 seconds and run the gate again, at most
      three times; then say it has not finished starting and stop.
    - otherwise read `{"type":"config_entries/get","domain":"marketplace"}`:
      `disabled_by` set → it is disabled: enable it under Settings > Devices
      & services > Marketplace; `state` `setup_in_progress` → still
      loading: wait as above; `setup_retry` → Home Assistant retries on its
      own: give its `reason` and suggest trying again later; `setup_error`
      → give its `reason` (a GitHub connection that no longer works:
      reconnect it under that entry's ⋮ > Reconfigure; anything else: the
      logs); no entry → Home Assistant sets it up again at its next restart;
      anything else → the Marketplace did not start: the Home Assistant logs.

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
  `downloads`, `last_updated`. Use the list row's `domain` for
  domain-dependent steps; reserve `hacs/repository/info` for the fields
  it exclusively owns (releases, `selected_tag`, `beta`,
  `default_branch`, README) — its first read forces a GitHub refresh and
  clears the repository's `new` flag, so a read-only preview that calls
  it mutates HACS UI state.
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
  registers a custom repository; does NOT download. Refused raw (the user
  adds custom repositories in the HACS UI); kept here for reference. The socket cannot
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

## Marketplace mode (Home Assistant 2026.11+)

Home Assistant 2026.11 ships HACS as the built-in Marketplace
(`marketplace` integration, Settings > Marketplace, set up by default;
verified on 2026.11.0b4). On its first start it takes over a HACS
installation: downloads stay installed and keep getting updates, custom
repositories and update entities carry over (same entity IDs; their registry
`platform` becomes `marketplace`), and the HACS integration is removed — it
cannot be installed again, and every `hacs/*` command answers
`unknown_command`.

`{"type":"marketplace/info"}` → `categories` (`integration`, `plugin`, `theme`,
`template`), `disabled_reason`, `github_connected`, `has_pending_tasks`,
`lovelace_mode`, `stage`, `startup`, `version` (Home Assistant's),
`warning_accepted` (for the Relay's own user only). A set `disabled_reason` →
the Marketplace is not operational (inventory reads may still answer): say so
with its next step — `rate_limit`: GitHub's request limit; wait for it to
reset, or connect GitHub for a higher limit; `invalid_token`: reconnect GitHub
under Settings > Devices & services > Marketplace > ⋮ > Reconfigure.

Beyond that gate, HA NOVA reads one thing here:
`{"type":"marketplace/repositories/list","categories":[...]}` (categories
optional) → the whole catalog, thousands of rows: always filter with
`--jq-file` (installed only: `[.data[] | select(.installed == true)]`).
Rows carry `id`, `name`, `full_name`,
`category`, `custom`, `installed`, `installed_version`, `available_version`,
`pending_upgrade`, `status` (`pending-restart` = installed, not yet active), and
`domain`. Answer inventory questions from it with the List Frame and lifecycle
wording of SKILL.md → Inventory; a `pending_upgrade` row names the waiting version.

Everything else is not supported through HA NOVA yet: install, update,
redownload, another version, prereleases, uninstall, custom repositories, and
migrations. Say so plainly and send nothing beyond the two reads above — no
other `marketplace/*` command, no `update.install`, no `hacs/*` command, no
file or `.storage` edit — then give the user the steps in Home Assistant. The
first time each admin opens Settings > Marketplace it shows a risk notice;
their installs and updates (Settings > Updates too) fail until they accept it.
- install: look the repository up in the catalog list first; listed →
  Settings > Marketplace > the repository > Install; not listed → a custom
  repository (below)
- another version: the repository page > ⋮ > Install another version (not
  for going back after an update: Home Assistant advises restoring a backup
  from before it)
- update: Settings > Updates; an updated integration needs a restart, which
  a repair under Settings > System > Repairs offers
- uninstall: the repository page > ⋮ > Uninstall (an integration that is
  still set up: remove its setup first, or pick Delete and uninstall, which
  also deletes its devices and entities)
- custom repository: Settings > Marketplace > Add from link (needs a GitHub
  connection); remove one there under Added from links
- prereleases: the repository's Pre-release switch on its device under
  Settings > Devices & services > Marketplace (the entity is disabled by
  default: enable it first, then turn the switch on)

AppDaemon apps and Python scripts are no longer managed: their files stay and
keep working without updates (a repair names them).
