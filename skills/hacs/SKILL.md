---
name: hacs
description: Use when installing, version-pinning or downgrading, redownloading, or removing HACS packages, adding custom HACS repositories, or migrating to a custom integration through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core). HACS 2.x must already be installed in Home Assistant.
---

# HA NOVA HACS Lifecycle

## Scope

HACS package lifecycle:
- inventory: registered repositories, installed versions, pending states
- custom repository registration and removal
- download/install (exact version, stable, or explicit prerelease), update,
  redownload, version pinning
- uninstall/removal with consumer discovery
- custom-integration migration coordination

Not in scope:
- `update.*` entity flows — the plain "update this integration" path stays in
  `ha-nova:updates`; this skill owns what the entity flow cannot do
- integration config flows after install (`ha-nova:integration-setup`)
- installing HACS itself (HA documentation/UI)
- Lovelace dashboard editing (`ha-nova:dashboard`)

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

File-based relay requests only:
- `ha-nova relay ws --data-file <payload-file> --out <result-file>` — all
  `hacs/*` commands, plus the WS-only frontend reads
  `{"type":"lovelace/resources"}` and
  `{"type":"lovelace/config","url_path":<dashboard>}`
- `ha-nova relay core --method GET --path <path> --out <result-file>` for HA
  REST reads (config entries, states)
- `ha-nova relay core --method DELETE --path /api/config/config_entries/entry/<entry_id>`
  — ONLY for a dependent config-entry deletion inside this skill's
  uninstall/migration flow, after its own typed confirmation
- `--jq-file <filter-file>` for non-trivial filters

Envelope parsing follows `skills/ha-nova/relay-api.md` → Standard Envelope;
WS results live under `.data`.

The pinned command map, repository-identity rules, error asymmetry, and
restart semantics live in `hacs-commands.md` — read it before the first
`hacs/*` call. Only commands from that map may be sent. Read `hacs/info`
first every session and apply its capability gate; on an unrecognized schema
or a set `disabled_reason`, fail closed with the HACS-UI pointer on an
unrecognized install. Never guess schemas, never edit `.storage`, never SSH.

## Flow

### Inventory

1. Capability gate (`hacs/info`), then `hacs/repositories/list` — bounded:
   filter by requested category/name, render as a List Frame with counts and
   Progressive Detail; never dump the raw list.
2. Registration, downloaded files, and config entries are three distinct
   lifecycle objects — name them as such in every result (a registered
   repository is not installed; installed files are not a configured
   integration).
3. Surface `hacs/repositories/removed` matches and unacknowledged
   `hacs/critical/list` entries for installed packages as warnings.

### Resolve

Resolve the target through `list`/`hacs/repository/info` in THIS session and
use only its `id` afterwards (identity rules in `hacs-commands.md`). On
ambiguity, ask one blocking question with the candidate `full_name`s. Never
act on a name match alone.

### Install / update / redownload / pin

1. Read `hacs/repository/releases` (and `release_notes` for updates) before
   choosing. A user-pinned version (`selected_tag`) is never silently
   replaced by a newer release — say the pin exists and ask; prerelease only
   on explicit request — choose the tag from `releases` (its `prerelease`
   flag marks them; no visibility toggle needed to install one). The
   `hacs/repository/beta` toggle is itself a mutation: send it only after
   the confirmed preview, when the user wants prereleases to STAY visible.
2. Preview per the Preview Card: repository `full_name`, category,
   installed → target version, HA domain, restart/reload impact, and — for
   updates — the release-notes summary with breaking changes. Natural
   confirmation for install/update/redownload binds to this exact preview.
3. Apply: `hacs/repository/download` with the chosen `version` — never
   version-then-download (a version-less download resolves to LATEST, and
   every download clears the selection; see `hacs-commands.md`). Branch-only
   repositories (`releases` empty, `version_or_commit` = `commit`) are the
   one case WITHOUT a `version`: preview the default-branch install, send
   `download` without `version`, and verify against the commit-ish
   `installed_version` — pinning and downgrades are unavailable there; say
   so instead of blocking the install. A Relay
   timeout is an UNKNOWN outcome — enter the reconcile loop below; never
   fire a second download on a timeout. For a persistent pin, send
   `hacs/repository/version` AFTER the verified download and disclose that
   HACS keeps flagging the repository `pending_upgrade`.
4. Verify category-appropriately (Verification below) and report INSTALLED
   vs ACTIVE honestly.

### Custom repositories

- Add: validate the GitHub reference, then `hacs/repositories/add`. The
  socket reports success EVEN ON FAILURE — verify by re-listing for the
  canonical `full_name`; missing after the re-read means the add failed
  (duplicate/rename detection happens on the same re-read). Registration
  does not download anything; say so.
- Remove registration: typed confirmation code, and only with exact identity
  plus installation state known — and only when `installed == false`.
  Unregistering an installed repository strands its files (the ID vanishes
  from `list`, so a later uninstall would need re-registration): when it is
  still installed, run the separately confirmed uninstall FIRST, then
  unregister. Removing a registration never deletes files.

### Uninstall / removal

EVERY uninstall/removal — standalone or inside a migration — runs
HACS-specific consumer discovery FIRST and shows the result in the preview:
- the integration's OWN footprint through the HA registries: entities whose
  registry `platform` equals the integration's domain (this also covers
  YAML-configured setups that have NO config entry), config entries of that
  domain (`config_entries/get`) with their devices, plus a bounded
  `/api/states` scan for the domain's entities missing from the registry.
  When none of these sources yields the footprint, the preview says the
  footprint is incomplete — never that there is none
- REFERENCING automations/scripts/scenes through `search/related` on those
  entities — read the response ONLY through the canonical filter
  `skills/ha-nova/search-related-consumers.jq` (recreate it per
  `skills/ha-nova/relay-api.md` → Parsing rule on flat-copy installs). Fail
  closed: only a verified-shape empty result means "no linked consumers
  found"; a failed, skipped, or wrong-path scan renders as "consumer check
  inconclusive", NEVER as a no-consumer claim. The canonical filter covers
  exactly those three referencer arrays — helpers and other referencer
  kinds are disclosed as unscanned
- for frontend packages additionally the Lovelace resource list and storage
  dashboard configs for the package's custom element references; manual YAML
  dashboards and template consumers are not enumerable — name them as an
  unscanned surface, never claim "no consumers"
Then the Delete Card with the typed `confirm:<token>`; dependent
config-entry deletion carries its own typed confirmation. Record pre-change
state (versions, repository identity) in the result so reinstalling the
previous version stays explainable.

### Migration (custom integration replaces another)

A CURRENT completed full Home
Assistant Backup is MANDATORY before the first destructive step — create
one via `ha-nova:backup`, or accept an existing backup only when it is
COMPLETED and CURRENT (a backup predating the present configuration does
not satisfy the gate; config entries sit outside config snapshots — the
Backup is the recovery net).
Check same-domain replacement risk before installing the replacement: a
fork that keeps the original integration domain collides with the
installed one — name which registration/entry survives and in what order.
Never delete a working config entry before the replacement is resolvable;
hand config flows to `ha-nova:integration-setup`; hard stop for UI-only
credentials/OAuth/pairing; entity-ID preservation is verified, never
promised; cleanup of the old package is delayed until verification passes.

### Reconcile loop (timeouts and unknown outcomes)

A Relay timeout or generic error is an UNKNOWN outcome, never a failure:
re-read HACS repository state, HA config entries, and pending flows over a
bounded settle window (up to three reads across ~30 seconds) until the
outcome is proven completed/failed/still-running. A single unchanged
read-back proves nothing — the upstream operation may still land later.
Non-idempotent mutations NEVER auto-retry; only an idempotent mutation
(repeating it is harmless even if the original lands later) may retry, and
only after re-reading identity before every retry. Everything else stops
with the observed state and asks the user.

### Verification

Command success alone is never a result. Verify by category:
- integration: repository `installed`/`installed_version`, manifest/domain
  presence, config-entry state — distinguishing INSTALLED (files updated)
  from ACTIVE (running component). Every integration upgrade requires a
  Home Assistant restart before the new version is live: report "installed,
  not yet active", surface the pending-restart status/repair, and offer the
  restart as a separate confirmed step via `ha-nova:service-call` — never
  restart silently, never claim the new version is live before it.
- frontend/plugin: repository state and installed version; in storage
  resource mode also the Lovelace resource registration — in YAML mode the
  resource is user-managed: say so instead of failing the check. A
  frontend reload note replaces restart talk.
- theme/template/python_script/appdaemon: repository state and version.
Also verify survival of unrelated objects and absence of duplicates after
adds/migrations. Evidence comes from the HACS/HA APIs ONLY — never file
reads; the Relay file endpoint denies `custom_components` and `www` by
design.

## Error Handling

Apply the error asymmetry from `hacs-commands.md`: clean
`repository_not_found` only from `info`/`ignore`; `download` errors with
code `error`; most bad-ID paths return a generic error — treat any generic
error as an unknown outcome (reconcile loop), not as proof of failure, and
re-resolve the ID before anything else. Never surface raw envelopes; report
the operation, the observed state, and the next safe step.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output: List Frame
for inventories (counts first, Progressive Detail follow-ups), Preview/Delete
/Result Cards for mutations, no raw JSON. Keep repository `full_name`,
versions, categories, and HA state values literal; localize everything else
at runtime.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Confirmation tiers: natural confirmation for install/update/redownload;
  the typed confirmation code for uninstall, registration removal, and
  dependent config-entry deletion.
- The migration backup gate is non-negotiable: no destructive migration
  step before a COMPLETED and CURRENT full Home Assistant Backup exists.
- Never trigger restarts, reloads, or config flows silently — each is its
  own confirmed step in its owning skill.
- No credentials or GitHub secrets in chat or output, ever.
