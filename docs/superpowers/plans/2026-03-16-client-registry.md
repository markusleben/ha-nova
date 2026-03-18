# Client Registry Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardcoded current-client policy with one small registry that drives wizard choices, install/uninstall behavior, and client-doc contracts for the four real HA NOVA targets.

**Architecture:** Add one checked-in JSON registry file plus a tiny typed loader and three fixed adapter kinds. Migrate only Claude, Codex, OpenCode, and Gemini in MVP. Keep Cursor / VS Code out of the runtime registry until they have a real adapter contract.

**Tech Stack:** Go CLI, JSON registry file, Vitest contracts, Go tests

---

## Chunk 1: Lock the Registry Contract

### Task 1: Add the source-of-truth contract

**Files:**
- Create: `tests/onboarding/client-registry-contract.test.ts`
- Modify: `tests/onboarding/client-install-docs-contract.test.ts`
- Reference: `docs/superpowers/specs/2026-03-16-client-registry-design.md`

- [ ] **Step 1: Write failing contract tests**
  - assert every registry entry in `clients/registry.json` has:
    - `id`
    - `label`
    - `adapter_kind`
    - `supported_os`
    - `install_doc`
    - `availability`
  - assert `adapter_kind` is one of:
    - `plugin_marketplace`
    - `skill_tree`
    - `skill_flat`
  - assert only `availability=ga` targets are expected in onboarding/runtime tests

- [ ] **Step 2: Run focused contract test**

Run: `npx vitest run tests/onboarding/client-registry-contract.test.ts`
Expected: FAIL because no registry exists yet

## Chunk 2: Add the Tiny Registry

### Task 2: Add typed registry loader

**Files:**
- Create: `cli/client_registry.go`
- Create: `cli/client_registry_test.go`
- Create: `clients/registry.json`

- [ ] **Step 1: Implement a tiny loader**
  - parse `clients/registry.json`
  - preserve registry order
  - reject unknown adapter kinds
  - expose helpers:
    - `loadClientRegistry()`
    - `supportedClientsForOS(goos string)`
    - `lookupClient(id string)`

- [ ] **Step 2: Write unit tests**
  - valid registry entries load
  - invalid adapter kind fails
  - unsupported OS targets filtered out
  - non-`ga` entries excluded from runtime selection

- [ ] **Step 3: Run focused Go tests**

Run: `cd cli && go test ./... -run 'TestLoadClientRegistry|TestSupportedClientsForOS|TestLookupClient'`
Expected: PASS

## Chunk 3: Migrate the Runtime

### Task 3: Replace wizard hardcoding

**Files:**
- Modify: `cli/setup_ui.go`
- Modify: `cli/setup_interactive.go`
- Modify: `cli/setup_ui_test.go`
- Modify: `cli/setup_interactive_test.go`

- [ ] **Step 1: Replace hardcoded setup choices with registry-backed choices**
- [ ] **Step 2: Keep the same UX order and numbering semantics**
- [ ] **Step 3: Preserve `all` as a synthetic choice built from `ga` registry entries**
- [ ] **Step 4: Run focused tests**

Run: `cd cli && go test ./... -run 'TestPromptSetupClient|TestInteractiveSetup'`
Expected: PASS

### Task 4: Replace install/uninstall switchboard

**Files:**
- Modify: `cli/clients.go`
- Modify: `cli/commands.go`
- Modify: `cli/state.go`
- Modify: `cli/clients_test.go`
- Modify: `cli/uninstall_test.go`

- [ ] **Step 1: Resolve client install/remove behavior from registry adapter_kind**
- [ ] **Step 2: Keep existing built-in adapter implementations; only swap dispatch source**
- [ ] **Step 3: Keep unknown client ids as loud errors**
- [ ] **Step 4: Run focused tests**

Run: `cd cli && go test ./... -run 'TestInstallClaudePlugin|TestInstallGeminiClient|TestRunUninstall'`
Expected: PASS

## Chunk 4: Align Docs + Contracts

### Task 5: Move client-doc contracts to the registry

**Files:**
- Modify: `tests/onboarding/client-install-docs-contract.test.ts`
- Modify: `docs/reference/skill-architecture.md`
- Modify: `.claude/INSTALL.md`
- Modify: `.codex/INSTALL.md`
- Modify: `.opencode/INSTALL.md`
- Modify: `.gemini/INSTALL.md`

- [ ] **Step 1: Read client docs through registry entries where possible**
- [ ] **Step 2: Keep README product-level and client docs client-level**
- [ ] **Step 3: Keep Windows claims aligned with real support proof**
- [ ] **Step 4: Run focused contract tests**

Run: `npx vitest run tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/client-registry-contract.test.ts`
Expected: PASS

## Chunk 5: Guard the Future Without Shipping It

### Task 6: Document future editor targets without pretending support

**Files:**
- Modify: `docs/superpowers/specs/2026-03-16-client-registry-design.md`
- Modify: `docs/releasing.md`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] **Step 1: Document the rule that Cursor / VS Code stay out of the runtime registry until a concrete adapter exists**
- [ ] **Step 2: Record likely future adapter family needs: MCP/prompt/plugin bundle**
- [ ] **Step 3: Make no runtime code change for those future targets in MVP**

## Chunk 6: Verify + Review

### Task 7: Full verification

**Files:**
- Modify: none expected

- [ ] **Step 1: Run focused Go tests**

Run: `cd cli && go test ./...`
Expected: PASS

- [ ] **Step 2: Run focused JS contracts**

Run: `npx vitest run tests/onboarding/client-registry-contract.test.ts tests/onboarding/client-install-docs-contract.test.ts tests/onboarding/install-skills-per-client.test.ts`
Expected: PASS

- [ ] **Step 3: Run full verification**

Run: `npm run verify`
Expected: PASS

## Recommended sequencing

Do this **after** the remaining direct UX parity fixes:

1. LLAT walkthrough
2. uninstall preflight / relay-running note
3. client registry refactor

Reason:
- the first two are small behavior fixes
- the registry is a wider structural change touching the same surfaces
