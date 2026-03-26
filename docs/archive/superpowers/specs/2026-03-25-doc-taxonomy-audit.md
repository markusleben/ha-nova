## Summary

- Active doc surface is too wide.
- Main problem is not missing docs.
- Main problem is too many active-looking docs carrying historical or duplicated truth.

Historical pre-archive size:
- `docs/` + `skills/`: 138 Markdown files
- former superpowers specs path: 64 files
- former superpowers plans path: 37 files

## Keep

- `README.md`
  - public product/install/support truth
- `CONTRIBUTING.md`
  - contributor workflow + architecture truth
- `PROJECT.md`
  - only if reduced to short internal product context
- `docs/releasing.md`
  - release/operator truth
- `docs/reference/ha-api-matrix.md`
  - HA transport truth
- `docs/reference/bridge-architecture.md`
  - relay surface truth
- `docs/reference/skill-architecture.md`
  - skill topology/index only
- `nova/DOCS.md`
  - HA app/relay operator doc
- skill `SKILL.md` files
  - runtime behavior/source docs

## Merge / Shrink

- `PROJECT.md`
  - remove install/support matrix and contributor workflow duplication
- `SUPPORT.md`
  - reduce to tiny routing page or fold into root/community-health docs
- `nova/README.md`
  - replace with pointer stub or fold into `nova/DOCS.md`
- release-channel / release-notes / release-validation mini-spec cluster
  - fold into `docs/releasing.md`
- test/validation mini-spec cluster
  - fold into one active testing policy or into `docs/releasing.md`
- `docs/reference/skill-architecture.md`
  - keep as index/topology, not second behavioral spec

## Archive

- almost all archived superpowers plans
- almost all archived superpowers specs
- dated process/research docs under `docs/reference/` that are not active truth
- `docs/breadcrumbs.md`
  - work journal, not active runbook
- old sections of `docs/choices.md`
  - keep only enduring defaults active

## Delete Candidates

- `docs/archive/plans/2026-03-07-guide-skill.md`
- `docs/archive/plans/2026-03-07-guide-skill-design.md`
- any plan/spec whose durable rule already lives in active docs/tests and whose removal changes no current maintainer decision

## Governance

Use only 3 doc classes:

1. `SSOT`
- active public/contributor/reference/runbook docs

2. `Work`
- one temporary working doc per active topic
- no plan/spec pair by default

3. `Archive`
- historical rationale, landed notes, journals

Rules:
- every active doc must have one clear owned truth surface
- everything else links instead of restating
- every new work doc must carry status:
  - `active`
  - `merged`
  - `superseded`
  - `abandoned`
- same PR that lands behavior must also:
  - update the active SSOT
  - update tests/contracts where needed
  - archive or delete the work doc
- only active docs get contract tests
- archive docs must never be normative

## Highest-Risk Redundancies

- install/support truth repeated across:
  - `README.md`
  - `PROJECT.md`
  - `CONTRIBUTING.md`
  - per-client install docs
  - `docs/releasing.md`
- release truth repeated across many mini-specs instead of one runbook
- decisions/history stored as active docs via:
  - `docs/archive/superpowers/specs/*`
  - `docs/archive/superpowers/plans/*`
  - `docs/breadcrumbs.md`
  - `docs/choices.md`
