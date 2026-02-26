# One-Link Codex Install Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Provide a single public instruction link for Codex that guides end users through HA NOVA onboarding with minimal friction and explicit validation.

**Architecture:** Use a docs-first entrypoint (`.codex/INSTALL.md`) that orchestrates existing onboarding commands (`setup`, `doctor`, `codex`, `claude`) instead of introducing new runtime logic. Keep the onboarding script as the only executable source of truth for validation and secret handling. Ensure copy-paste UX parity with popular skill installers while preserving App + Relay terminology.

**Tech Stack:** Markdown docs, Bash onboarding script (`scripts/onboarding/macos-onboarding.sh`), Vitest contract tests.

---

### Task 1: Establish canonical install entrypoint

**Files:**
- Create: `.codex/INSTALL.md`
- Modify: `.codex/ONBOARDING.md`

**Step 1: Define the canonical URL target**

- Canonical path: `.codex/INSTALL.md`
- Preserve `.codex/ONBOARDING.md` as compatibility alias.

**Step 2: Write install instructions with explicit input expectations**

- Include:
  - prerequisites
  - `npm run onboarding:macos`
  - expected user prompts (HA URL/host, Relay URL, Relay auth token, optional LLAT)
  - `doctor` verification
  - launch commands
- Include actionable error mapping (`401/403`, `404`, `000`).

**Step 3: Make ONBOARDING alias deterministic**

- Replace content with short pointer to canonical `INSTALL.md`.

**Step 4: Verification**

Run:
```bash
test -f .codex/INSTALL.md && test -f .codex/ONBOARDING.md
```

Expected: exit code `0`.

### Task 2: Wire docs to one-link pattern

**Files:**
- Modify: `docs/user-onboarding-macos.md`

**Step 1: Use canonical raw URL in docs**

- Set Codex prompt to:
  - `https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md`

**Step 2: Keep docs minimal**

- Keep manual setup section for users not using Codex.
- Do not duplicate script internals already documented elsewhere.

**Step 3: Verification**

Run:
```bash
rg -n "raw.githubusercontent.com/.*/.codex/INSTALL.md|.codex/ONBOARDING.md" docs/user-onboarding-macos.md .codex/INSTALL.md .codex/ONBOARDING.md
```

Expected: canonical `INSTALL.md` URL present.

### Task 3: Contract guard for docs UX

**Files:**
- Modify: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Add docs contract assertions**

- Assert `docs/user-onboarding-macos.md` references `.codex/INSTALL.md`.
- Assert `.codex/INSTALL.md` includes:
  - `npm run onboarding:macos`
  - `bash scripts/onboarding/macos-onboarding.sh doctor`

**Step 2: Run test**

Run:
```bash
npm test -- tests/onboarding/macos-onboarding-script-contract.test.ts
```

Expected: PASS.

### Task 4: Record decisions and breadcrumbs

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

**Step 1: Record decision**

- Canonical one-link onboarding entrypoint is `.codex/INSTALL.md`.
- `.codex/ONBOARDING.md` remains alias.

**Step 2: Record work log**

- Add concise bullet list with date category.

**Step 3: Verification**

Run:
```bash
tail -n 40 docs/choices.md
tail -n 60 docs/breadcrumbs.md
```

Expected: new one-link entries visible.

### Task 5: End-to-end dry run in current repo state

**Files:**
- Read-only verification

**Step 1: Validate docs and script commands**

Run:
```bash
bash scripts/onboarding/macos-onboarding.sh --help
bash scripts/onboarding/macos-onboarding.sh doctor || true
```

Expected:
- help includes `setup`, `doctor`, `codex`, `claude`
- doctor prints actionable status lines

**Step 2: Validate test suite slice**

Run:
```bash
npm test -- tests/onboarding/macos-onboarding-script-contract.test.ts
```

Expected: PASS.
