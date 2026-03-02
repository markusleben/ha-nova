# HA NOVA Consent + Fast-Path + Block Output Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce time-to-first-proposal, simplify create/update confirmations, and enforce compact block output while keeping destructive safety strict.

**Architecture:** Update skill contracts first (Markdown source-of-truth), then align TypeScript contract helpers and tests. Keep Relay/server runtime untouched. Apply changes to both repo skill docs and installed `.agents` mirror for contract parity.

**Tech Stack:** Markdown skill contracts, TypeScript contract helpers, Vitest.

---

### Task 1: Consent and orchestration contract updates

**Files:**
- Modify: `skills/ha-nova/core/contracts.md`
- Modify: `skills/ha-nova/core/blocks.md`
- Modify: `skills/ha-nova.md`
- Modify: `.agents/skills/ha-nova/SKILL.md`

**Steps:**
1. Add consent tiers (`read`, `create/update`, `destructive`).
2. Allow natural confirmation for `create/update`; keep token for `delete/destructive`.
3. Add preview binding requirement (`preview_id`, `preview_digest`, TTL).
4. Update subagent rule for simple flows: serial fast-path default.

### Task 2: Domain output + safety docs alignment

**Files:**
- Modify: `skills/ha-safety.md`
- Modify: `skills/ha-automation-crud.md`
- Modify: `skills/ha-nova/modules/automation/create-update.md`
- Modify: `skills/ha-nova/modules/script/create-update.md`

**Steps:**
1. Replace legacy field labels with block output contract v1.
2. Add decision-guiding requirement before write for ambiguous behavior.
3. Enforce delete verify-absent read-back as mandatory.

### Task 3: TDD for consent helpers

**Files:**
- Modify: `tests/skills/ha-token-contract.test.ts`
- Modify: `src/skills/contracts/confirm-token.ts`

**Steps:**
1. Write failing tests for natural create/update confirmation and strict destructive confirmation.
2. Implement minimal parser/validator changes.
3. Keep existing token binding checks intact.

### Task 4: Contract test alignment

**Files:**
- Modify: `tests/skills/ha-nova-contract.test.ts`
- Modify: `tests/skills/ha-safety-contract.test.ts`

**Steps:**
1. Update assertions to new block contract and consent model.
2. Add checks for serial fast-path wording and destructive token requirement.

### Task 5: Verification and breadcrumbs

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

**Steps:**
1. Record opinionated defaults and superseded policies.
2. Run focused Vitest suites and confirm green.
