# Private RC Installer Overrides Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add maintainer-only installer overrides so macOS and Windows can test real installer flows against private/local bundles.

**Architecture:** Keep user-facing install behavior unchanged. Add two explicit environment-variable overrides for bundle and checksum URLs, derive installed version from `bundle.json`, and verify version consistency before install.

**Tech Stack:** Bash, PowerShell, Vitest

---

### Task 1: Unix installer override path

**Files:**
- Modify: `install.sh`
- Test: `tests/onboarding/installer-contract.test.ts`

- [ ] Add bundle/checksum URL helpers for override and normal release URLs
- [ ] Skip GitHub latest lookup when override mode is active without `HA_NOVA_VERSION`
- [ ] Read installed version from `bundle.json` and validate it
- [ ] Extend Unix installer contract tests for the override path

### Task 2: Windows installer override path

**Files:**
- Modify: `install.ps1`
- Test: `tests/onboarding/windows-installer-contract.test.ts`

- [ ] Add bundle/checksum URL helpers for override and normal release URLs
- [ ] Skip GitHub latest lookup when override mode is active without `HA_NOVA_VERSION`
- [ ] Read installed version from `bundle.json` and validate it
- [ ] Extend Windows installer contract tests for the override path

### Task 3: Release docs

**Files:**
- Modify: `docs/releasing.md`

- [ ] Add a private RC test section that uses local/private bundle URLs
- [ ] Keep public release guidance unchanged for end users

### Task 4: Verification

**Files:**
- Test: `tests/onboarding/installer-contract.test.ts`
- Test: `tests/onboarding/windows-installer-contract.test.ts`
- Test: `tests/onboarding/release-contract.test.ts`

- [ ] Run targeted contract tests
- [ ] Run `npm run verify`
