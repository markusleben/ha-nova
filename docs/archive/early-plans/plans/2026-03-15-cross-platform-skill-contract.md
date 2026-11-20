# Cross-Platform Skill Contract Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make HA NOVA onboarding and later skill usage genuinely cross-platform by removing bash-only relay assumptions from active skills and by stopping Claude plugin drift from the installed payload.

**Architecture:** Keep the Go runtime contract as-is and fix the instruction/install layer around it. Introduce one shell-agnostic relay usage contract for skills (`--data-file`, `--body-file`, `--out`), tighten the HA NOVA context skill, and make the Claude plugin registration/update path source the installed payload rather than the GitHub repo.

**Tech Stack:** Markdown skills, Go CLI, Claude plugin metadata, Vitest contract tests, Go unit tests

---

## Chunk 1: Claude Drift Contract

### Task 1: Lock Claude plugin sourcing to the installed payload

**Files:**
- Modify: `.claude-plugin/marketplace.json`
- Modify: `cli/clients.go`
- Test: `tests/onboarding/install-skills-per-client.test.ts`
- Test: `tests/onboarding/desktop-validation-contract.test.ts`

- [ ] **Step 1: Write failing contract coverage proving Claude install/update must not drift to the GitHub repo source**
- [ ] **Step 2: Update plugin marketplace/install logic so the supported Claude path resolves to the installed payload**
- [ ] **Step 3: Tighten error handling so supported Claude registration/update does not silently succeed on failure**
- [ ] **Step 4: Run focused Claude install/update tests**

## Chunk 2: Shell-Agnostic Relay Contract

### Task 2: Define the canonical relay usage pattern for skills

**Files:**
- Modify: `skills/ha-nova/SKILL.md`
- Modify: `skills/ha-nova/relay-api.md`
- Modify: `skills/onboarding/SKILL.md`
- Test: `tests/skills/ha-nova-contract.test.ts`

- [ ] **Step 1: Replace bash-only runtime guidance with shell-agnostic runtime guidance**
- [ ] **Step 2: Document `--data-file`, `--body-file`, and `--out` as the default cross-platform relay contract**
- [ ] **Step 3: Keep shell-specific snippets optional, not canonical**
- [ ] **Step 4: Run focused context/relay-contract tests**

### Task 3: Migrate active skills off bash-first relay instructions

**Files:**
- Modify: `skills/read/SKILL.md`
- Modify: `skills/review/SKILL.md`
- Modify: `skills/helper/SKILL.md`
- Modify: `skills/entity-discovery/SKILL.md`
- Modify: `skills/fallback/SKILL.md`
- Modify: `skills/service-call/SKILL.md`
- Modify: `skills/ha-nova/safe-refactoring.md`
- Test: `tests/skills/ha-nova-contract.test.ts`
- Test: add new skill-contract coverage if needed

- [ ] **Step 1: Replace inline `relay ws -d '...'` defaults with file-based relay payload patterns**
- [ ] **Step 2: Replace Unix-only temp sinks/redirection as the default path**
- [ ] **Step 3: Remove backslash-continuation/bash-pipeline requirements from the canonical path**
- [ ] **Step 4: Run focused skill contract tests**

## Chunk 3: Client Docs

### Task 4: Tighten client docs without bloating the README

**Files:**
- Modify: `.claude/INSTALL.md`
- Modify: `.codex/INSTALL.md`
- Modify: `.gemini/INSTALL.md`
- Modify: `.opencode/INSTALL.md`
- Modify: `README.md`

- [ ] **Step 1: Keep README at product level only**
- [ ] **Step 2: Put Claude Windows installer-migration/prerequisite notes only into `.claude/INSTALL.md`**
- [ ] **Step 3: Make all client install docs use shell-appropriate command fences for Windows vs macOS/Linux**
- [ ] **Step 4: Run doc contract checks if present**

## Chunk 4: Verification + Real Smoke

### Task 5: Verify the new cross-platform contract

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`
- Modify: `docs/releasing.md`

- [ ] **Step 1: Record the cross-platform relay-contract decision**
- [ ] **Step 2: Record the Claude installed-payload sourcing decision**
- [ ] **Step 3: Run `npm run verify`**
- [ ] **Step 4: Run Windows real smoke: onboarding -> doctor -> one real skill call**
- [ ] **Step 5: Run macOS real smoke: onboarding -> doctor -> one real skill call**
- [ ] **Step 6: Fresh-review the diff before any completion claim**
