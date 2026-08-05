# HACS Lifecycle Spec (#478)

Status: active
Scope: first-class, safe HACS lifecycle management. Supersedes the archived
masterplan's "Deliberately NOT: HACS management parity" entry — revised by the
maintainer in #478 with the solaredgeoptimizers migration as the reference
case. Sequencing: Phase 3 item 1 in
[2026-08-03-backlog-sequencing.md](2026-08-03-backlog-sequencing.md).

## Architecture decision: skill-first, no Relay delta

HACS exposes its commands over the Home Assistant WebSocket API, and the
Relay's `/ws` endpoint is a generic proxy — the transport already exists.
The charter stays intact: the Relay learns nothing about HACS; every piece of
intelligence lives in a new `skills/hacs/` skill.

The two hard problems are handled skill-side:

1. **Private, version-dependent schemas.** The skill ships a pinned command
   map for the supported HACS major line (currently `hacs/repositories/*`,
   `hacs/repository/*` families), guarded by capability detection: read the
   HACS version first (config-entry/manifest or `hacs/info`), proceed only on
   a recognized schema version, and fail closed with the HACS-UI pointer on an
   unrecognized one. The probe itself sits behind an ADMIN gate: HACS
   registers `hacs/info` and every repository list/mutation command
   admin-only, so a Relay backed by a non-admin credential (standalone
   LLAT of a regular user, non-admin Cloud OAuth user) fails the probe
   with WS error `unauthorized` — a PERMISSION class, never evidence
   about HACS presence. The skill distinguishes the two probe failures:
   `unknown_command` → HACS is missing, not loaded, OR an unsupported
   line that does not register the pinned commands — one signal, three
   states, so remediation needs a second read (HACS config entry /
   integration manifest via the API): entry present → unsupported-schema
   fail-closed path with the HACS-UI pointer; entry absent → install or
   restart guidance. `unauthorized` → credential lacks admin
   (remediation: switch the Relay upstream credential to an admin
   account) — and never advertises HACS coverage before the probe
   succeeds. Never guess schemas; never edit `.storage`; never SSH —
   the Safety Core binds every operation to the Relay path, so unrecognized
   schemas and failed operations stop with the HACS-UI pointer, full stop.
2. **Long-running operations.** A Relay timeout is an UNKNOWN outcome, never
   a failure: every mutation follows read → mutate → bounded reconcile loop
   (re-read HACS repository state, HA integration/manifest state via the
   API, config entries, pending flows) until the outcome is proven
   completed/failed/still-running. A single unchanged read-back after a
   timeout proves NOTHING — the Relay only abandons its wait while the
   upstream operation keeps running and can land later. Automatic retries
   are therefore forbidden after an unresolved timeout: the reconcile loop
   re-reads over a bounded settle window, and only an IDEMPOTENT
   mutation (repeating it is harmless even if the timed-out original
   lands later) may be retried without the user — identity present →
   done, otherwise retry. Non-idempotent mutations NEVER auto-retry: an
   absent identity after the settle window still proves nothing while the
   upstream operation may complete later, so they stop with the observed
   state and ask. Duplicate registrations, downloads,
   and config flows are prevented by re-reading identity before every
   retry.
   Filesystem evidence is explicitly UNAVAILABLE: the Relay's file endpoint
   denies `custom_components` and `www` by design (executable paths,
   `DENIED_SEGMENTS` in `nova/src/http/handlers/files-paths.ts`), so
   reconciliation never relies on file reads.

## Skill scope (skills/hacs/SKILL.md + reference files)

- **Inventory:** normalized, bounded repository listing — identity/full name,
  category, default vs custom, installed state + versions, stable/prerelease
  availability, pending restart/update, HA domain/manifest, downloadable/
  removable/blocked. Registration, downloaded files, and config entries are
  three distinct lifecycle objects, named as such in output.
- **Custom repository lifecycle:** validate + add by canonical GitHub
  reference (duplicate/rename detection, canonical-identity verification)
  PLUS a resolved repository category — `hacs/repositories/add` requires
  it, so the preflight derives the category from repository evidence
  (manifest/topics/structure), asks the user when the evidence is
  ambiguous, and binds the chosen category in the preview; never guess
  silently, never defer the category to mutation-time failure. Remove
  registration only with exact identity + installation state known.
- **Package lifecycle:** install exact version (a user-pinned version is
  never silently replaced by a newer release), install selected stable,
  prerelease only on explicit request, update, redownload, uninstall; read
  available releases before choosing; verify CATEGORY-appropriately after
  every mutation — integrations verify manifest/domain/version and
  config-entry state, distinguishing INSTALLED (repository/files updated)
  from ACTIVE (the running component, which requires a Home Assistant
  restart): the result names which one was proven and never claims the
  new version is live before that restart; frontend/plugin packages
  verify HACS repository state, installed version, and — in storage
  resource mode — the Lovelace resource registration (YAML resource mode
  manages resources by hand: say the resource is user-managed instead of
  failing the check), themes verify repository state and version
  (neither has an integration manifest); surface restart/frontend-reload
  needs without performing them silently. The HACS category enum is wider
  than these three: `appdaemon`, `python_script`, and `template`
  repositories have no config-entry or Lovelace-resource anchor the Relay
  API can verify (and filesystem evidence is unavailable by design), so
  the MVP handles them fail-closed as UNSUPPORTED categories — named as
  such in the probe/inventory output with the HACS-UI pointer for their
  lifecycle, never routed through a wrong verification branch and never
  reported installed/uninstalled without category-appropriate evidence.
- **Migration coordination:** HACS-specific consumer discovery before
  destructive steps — config entries, devices, entities, automations,
  scripts, and helpers through the canonical `search/related` filter, PLUS
  a frontend-package scan the standard preflight cannot provide: read the
  Lovelace resource list and storage dashboard configs for the package's
  custom element/resource references (manual YAML dashboards are not
  enumerable — that gap is disclosed, never claimed clean). Entities
  referenced only inside templates are equally not enumerable via
  `search/related` (the preflight's documented template limit) — the
  preview names template consumers as an unscanned surface instead of
  claiming no consumers. Same-domain
  replacement risk, entity-ID preservation honesty, hand-off to
  `integration-setup` for config flows, hard stop for UI-only
  credentials/OAuth/pairing, never delete a working config entry before
  the replacement is resolvable, delayed cleanup until verification.
- **Safety:** read-before-write everywhere; preview names repository,
  category, installed → target version, domain, restart impact; natural
  confirmation for install/update/redownload; typed `confirm:<token>` for
  uninstall/removal and dependent config-entry deletion — and EVERY
  uninstall/removal, standalone or inside a migration, runs the
  HACS-specific consumer discovery first, so live config entries,
  automations, or dashboards depending on the package surface in the
  preview instead of breaking silently; **mandatory safety
  backup gate for migrations** — create a full Home Assistant Backup before
  the first destructive step, or accept an existing one only when it is
  COMPLETED and CURRENT per the safety-backup contract (a backup predating
  the present configuration does not satisfy the gate), since config-entry create/delete
  sits outside the config-snapshot layer (community-log lesson); record
  pre-change state (versions, repository identity) so manual recovery or
  reinstalling the previous version is always explainable; no credentials in
  chat/output ever.
- **Verification:** read back canonical registration, exact installed
  version, manifest/integration state through the HACS/HA APIs (never file
  reads — `custom_components`/`www` are denied by the Relay's file
  endpoint), pending states, config-entry count/state, entity
  creation/preservation, survival of unrelated objects, absence of
  duplicates. Command success alone is never a result.

## Wiring

- `skills/fallback/SKILL.md`: HACS row moves External → Covered
  (`skills/hacs`); the custom-integration configuration section keeps owning
  non-HACS config APIs.
- Update ownership is explicit: `skills/updates/SKILL.md` keeps `update.*`
  ENTITY updates (including HACS-provided update entities — the plain
  "update this integration" path stays there), while the HACS skill owns
  everything the entity flow cannot do: version pinning/downgrades,
  redownload, prerelease switches, registration, uninstall/removal, and
  migrations. The implementation PR adds the matching deferral lines to
  both skills so neither claims the other's half.
- Context skill dispatch row + smallest-solution contract + output-rules
  Cards; capability map, safety matrix, and config-snapshots "NOT covered"
  notes updated consistently.
- Reference case: the solaredgeoptimizers fork migration becomes the worked
  example in the skill's migration reference.

## Delivery

Two PRs after this spec: (1) `skills/hacs/` + fallback/dispatch wiring +
contract tests (skills-only); (2) migration reference + live E2E scenario
addition. No Relay, CLI, or workflow changes; evidence class stays in the
"None" row.
