# Contributor Runtime Contract Cleanup Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Align contributor workflow, dev helpers, tests, and docs with the Go-first hard-cut runtime.

**Architecture:** Keep the end-user contract small and push shell-era helpers behind an explicit dev-only boundary. Update contributor verification to require both Node and Go checks, then rewrite stale tests and helper scripts so they match the new contract without widening the public surface again.

**Tech Stack:** TypeScript, Vitest, Go, Bash, PowerShell

---

### Task 1: Contributor Contract

**Files:**
- Modify: `CONTRIBUTING.md`
- Modify: `package.json`
- Modify: `PROJECT.md`

- [ ] Add explicit Go CLI verification to contributor docs.
- [ ] Add a single contributor verification script entrypoint in `package.json`.
- [ ] Remove or mark stale public-facing onboarding aliases.

### Task 2: Dev Helper Contract

**Files:**
- Modify: `scripts/smoke/ha-app-mvp-crud-smoke.sh`
- Modify: `scripts/e2e/codex-ha-nova-live-skill-e2e.sh`
- Modify: `scripts/e2e/codex-ha-nova-scenarios-e2e.sh`

- [ ] Stop using shell onboarding as the runtime source of truth.
- [ ] Load current config from the Go-first install layout where needed.
- [ ] Keep these scripts dev-only and consistent with the current CLI contract.

### Task 3: Test + Doc Cleanup

**Files:**
- Modify: `tests/app/mvp-automation-crud-smoke-contract.test.ts`
- Modify: `tests/e2e/codex-skill-live-contract.test.ts`
- Modify: `tests/onboarding/macos-onboarding-script-contract.test.ts`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Rewrite stale assertions that still treat shell onboarding aliases as public/current truth.
- [ ] Preserve useful dev-helper coverage where it still matters.
- [ ] Document the contributor-contract changes and verification evidence.
