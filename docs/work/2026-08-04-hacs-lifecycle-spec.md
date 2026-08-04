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
   unrecognized one. Never guess schemas; never edit `.storage` as the normal
   path; SSH stays a last resort requiring explicit user authorization.
2. **Long-running operations.** A Relay timeout is an UNKNOWN outcome, never
   a failure: every mutation follows read → mutate → bounded reconcile loop
   (re-read HACS repository state, HA integration/manifest state via the
   API, config entries, pending flows) until the outcome is proven
   completed/failed/still-running. A single unchanged read-back after a
   timeout proves NOTHING — the Relay only abandons its wait while the
   upstream operation keeps running and can land later. Automatic retries
   are therefore forbidden after an unresolved timeout: the reconcile loop
   re-reads over a bounded settle window, and only a mutation that is
   idempotent-verifiable by identity (the exact registration/download
   already present → done; provably absent after the settle window AND
   safe to repeat) may be retried without the user; anything else stops
   with the observed state and asks. Duplicate registrations, downloads,
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
  reference (duplicate/rename detection, canonical-identity verification),
  remove registration only with exact identity + installation state known.
- **Package lifecycle:** install exact version (a user-pinned version is
  never silently replaced by a newer release), install selected stable,
  prerelease only on explicit request, update, redownload, uninstall; read
  available releases before choosing; verify CATEGORY-appropriately after
  every mutation — integrations verify manifest/domain/version and
  config-entry state, distinguishing INSTALLED (repository/files updated)
  from ACTIVE (the running component, which requires a Home Assistant
  restart): the result names which one was proven and never claims the
  new version is live before that restart; frontend/plugin packages
  verify HACS repository state, installed version, and the Lovelace
  resource registration, themes verify repository state and version
  (neither has an integration manifest); surface restart/frontend-reload
  needs without performing them silently.
- **Migration coordination:** HACS-specific consumer discovery before
  destructive steps — config entries, devices, entities, automations,
  scripts, and helpers through the canonical `search/related` filter, PLUS
  a frontend-package scan the standard preflight cannot provide: read the
  Lovelace resource list and storage dashboard configs for the package's
  custom element/resource references (manual YAML dashboards are not
  enumerable — that gap is disclosed, never claimed clean). Same-domain
  replacement risk, entity-ID preservation honesty, hand-off to
  `integration-setup` for config flows, hard stop for UI-only
  credentials/OAuth/pairing, never delete a working config entry before
  the replacement is resolvable, delayed cleanup until verification.
- **Safety:** read-before-write everywhere; preview names repository,
  category, installed → target version, domain, restart impact; natural
  confirmation for install/update/redownload; typed `confirm:<token>` for
  uninstall/removal and dependent config-entry deletion; **mandatory safety
  backup gate for migrations** — create (or require) a full Home Assistant
  Backup before the first destructive step, since config-entry create/delete
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
