# Documentation Governance

This file defines the active documentation taxonomy for HA NOVA.

## Active SSOT Docs

Use these files as the current sources of truth:

- `README.md`
  - public product, install, support, and platform truth
- `CONTRIBUTING.md`
  - contributor workflow and contribution rules
- `PROJECT.md`
  - short internal product context only
- `SUPPORT.md`
  - thin active support and conduct-routing page
- `CODE_OF_CONDUCT.md`
  - active conduct policy and private reporting contract
- `docs/releasing.md`
  - release and validation runbook
- `docs/reference/bridge-architecture.md`
  - relay surface truth
- `docs/reference/ha-api-matrix.md`
  - HA transport/routing truth
- `docs/reference/skill-architecture.md`
  - skill topology and contributor index
- `docs/reference/client-integration.md`
  - client registry and setup capability truth
- `.claude/INSTALL.md`, `.codex/INSTALL.md`, `.antigravity/INSTALL.md`, `.opencode/INSTALL.md`, `.hermes/INSTALL.md`
  - client-specific install overlays; derived active docs that cover client deltas only and must point back to `README.md` for the shared install contract
- `nova/DOCS.md`
  - Home Assistant App / relay operator truth
- `nova/README.md`
  - active pointer doc that routes readers to `README.md` or `nova/DOCS.md`
- `skills/**/SKILL.md`
  - active skill/runtime behavior

## Allowed Doc Classes

Use only these classes:

### 1. SSOT

Active product, contributor, release, reference, and skill docs.

Rules:
- exactly one owned truth surface per active file
- other docs link instead of restating
- only active docs may be normative

### 2. Work

Temporary planning/spec files for currently active work only.

Rules:
- keep active work docs under `docs/work/`
- one work doc per topic by default
- avoid parallel `plan` + `spec` pairs unless there is a strong reason
- every work doc must declare status:
  - `active`
  - `merged`
  - `superseded`
  - `abandoned`
- same PR that lands the behavior must:
  - update the active SSOT
  - update tests/contracts where needed
  - archive or delete the work doc

### 3. Archive

Historical rationale, journals, old plans, old specs, research notes.

Rules:
- archive docs are not normative
- archive docs must not be linked as active instructions from public/contributor/runbook docs
- if a file needs a “historical only” disclaimer, it belongs in archive

## Path Policy

Active paths:
- root docs listed above
- `SUPPORT.md`
- `CODE_OF_CONDUCT.md`
- `docs/reference/`
- `docs/releasing.md`
- `docs/work/`
- `.claude/INSTALL.md`
- `.codex/INSTALL.md`
- `.antigravity/INSTALL.md`
- `.opencode/INSTALL.md`
- `.hermes/INSTALL.md`
- `nova/DOCS.md`
- `nova/README.md`
- `skills/**/SKILL.md`

Archive paths:
- `docs/archive/**`
- `docs/archive/superpowers/**`

Legacy archive:
- `docs/archive/superpowers/` contains historical superpowers plans/specs only
- do not create new active docs under `docs/archive/superpowers/`
- archive content there is historical only, not active product documentation

Historical journals:
- `docs/archive/breadcrumbs.md` is the long historical breadcrumb ledger
- keep the root `docs/breadcrumbs.md` short and current only

## Anti-Drift Rules

- Behavior changes update exactly one active source-of-truth doc first.
- Release/install/support/platform truth must not be duplicated across multiple active docs.
- Only active docs get contract tests.
- Historical journals such as long breadcrumb logs must not keep growing as active SSOT surfaces.

## Preferred Cleanup Direction

- shrink `PROJECT.md` to internal context only
- keep `SUPPORT.md` as a thin routing page or fold it away later
- reduce `nova/README.md` to a pointer stub or fold it into `nova/DOCS.md`
- move old plans/specs/research into `docs/archive/`
