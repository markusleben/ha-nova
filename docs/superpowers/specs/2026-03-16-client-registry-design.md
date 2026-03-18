# 2026-03-16 Client Registry Design

## Problem

Client handling is still too hardcoded:

- wizard client list
- OS support decisions
- install/uninstall behavior
- docs/tests around client-specific prerequisites
- validation/test harness assumptions

That makes contributor-friendly client additions harder than necessary and keeps policy scattered across code.

Current hardcoded surfaces:

- `cli/setup_ui.go`
- `cli/clients.go`
- `scripts/onboarding/install-local-skills.sh`
- `docs/reference/skill-architecture.md`
- `tests/onboarding/install-skills-per-client.test.ts`
- `tests/onboarding/client-install-docs-contract.test.ts`

## Goal

Replace the hardcoded client switchboard with a small, data-driven registry that:

- declares which clients exist
- declares which OSes are supported
- declares which installer adapter they use
- declares user-facing notes/docs
- keeps install behavior safe and predictable

## Constraints

- KISS
- DRY
- no arbitrary third-party code execution
- contributors should be able to add a new client mostly by adding data, not editing wizard logic everywhere
- Windows and macOS must use the same source of truth
- no fake extensibility: unsupported future targets must not appear as if they already work

## Reality Check: Current vs Future Targets

Today HA NOVA has four real installable targets:

- Claude Code
- Codex CLI
- OpenCode
- Gemini CLI

They all fit one of three existing install shapes:

- plugin marketplace registration
- skill tree install
- flat copied skills

Cursor / VS Code are different.

They are not just "another skills folder":

- Cursor: MCP-centered + editor-native customization surface
- VS Code / Copilot: MCP + prompt files + instructions + agent/plugin surfaces

So the registry must not pretend they are installable today via the same simple switch.
They should be considered future targets, but **not** be added to the wizard until we have a real install contract and tests.

## Options

### Option A: Keep the hardcoded switch

Pros:
- simplest code today

Cons:
- policy stays duplicated
- every new client needs touching multiple files
- OS support metadata remains scattered

### Option B: Single checked-in registry file + fixed adapter types

Keep one small source-of-truth file, for example `clients/registry.json`.

Each entry contains only the minimum policy we actually need:

- `id`
- `label`
- `adapter_kind`
- `supported_os`
- `install_doc`
- `availability`
- optional `note`

The runtime only supports a few built-in adapter kinds:

- `skill_tree`
- `skill_flat`
- `plugin_marketplace`

Pros:
- contributor-friendly
- policy unified
- safe: no arbitrary installer scripts in the registry
- wizard/install/uninstall/tests can all share the same registry

Cons:
- still needs code when a genuinely new adapter kind appears
- one small loader needed in Go

### Option C: Repo-local per-client manifests

Pros:
- more decentralized

Cons:
- too much file churn for four current targets
- more schema surface than the MVP needs
- no real value over one registry file yet

### Option D: Full external client-plugin system

Pros:
- maximum extensibility

Cons:
- overkill
- harder to secure
- harder to test
- unnecessary for current scope

## Decision

Choose **Option B**.

Use one checked-in JSON registry file with a tiny fixed set of installer adapters.

But keep the MVP scope strict:

- registry covers only **real installable targets**
- wizard lists only `availability=ga` targets supported on the current OS
- Cursor / VS Code are explicitly future targets, not MVP registry entries yet

## Proposed Shape

Add `clients/registry.json`.

Each entry should include:

- `id`
- `label`
- `adapter_kind`
- `supported_os`
- `install_doc`
- `availability`
- optional `note`

No YAML.
No per-client files in the first pass.
No future-shaped metadata unless runtime code already consumes it.

## Runtime Rules

- Wizard reads the registry, not a hardcoded list.
- Unsupported or non-GA clients are not shown in the wizard.
- Install/uninstall uses the adapter kind from the registry.
- Tests assert that every registry entry has docs and a valid adapter.
- Only `availability=ga` entries participate in install/uninstall/update loops.

## Adapter Rules

The registry stays declarative.

Entries may choose among built-in adapter kinds only:

- `plugin_marketplace`
  - example: Claude
- `skill_tree`
  - example: Codex, OpenCode
- `skill_flat`
  - example: Gemini

If a future target needs something else, that is a code change with tests.
The registry file may not embed shell snippets, commands, or arbitrary installer logic.

## Future Target Rule

Cursor / VS Code should be handled as future adapter work, not as immediate wizard entries.

Reasons:

- official integration surfaces are broader than today's skill install model
- they need explicit decisions about MCP, prompt files, instructions, hooks, or editor plugins
- pretending they fit current adapters would create false support claims

So the KISS rule is:

1. build the registry around today's four real targets
2. keep the schema minimal, not future-heavy
3. add Cursor / VS Code only when their concrete adapter contract exists

## Contributor Model

A normal new client addition should require:

1. add one registry entry
2. add docs file
3. if needed, add one small adapter implementation only when none of the existing adapter kinds fit
4. add/update focused contract tests

## Non-Goals

- no remote marketplace for HA NOVA clients
- no runtime download of client definitions
- no arbitrary shell snippets embedded in the registry
- no generic third-party plugin execution model
- no "planned" clients shown in the wizard before they truly work
