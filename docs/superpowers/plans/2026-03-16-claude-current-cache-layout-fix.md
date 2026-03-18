# Claude Current Cache Layout Fix Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix local Claude reinstall cleanup and the Go installed-bundle local payload staging so current real cache layouts and installed bundle roots do not break plugin reinstall on macOS.

**Architecture:** One small cache-root fix plus one small staged-payload filter in the Go local marketplace prep. No new abstraction layer.

**Tech Stack:** Go CLI, shell installer, Go/Vitest tests

---

## Chunk 1: Lock the Real Cache Layout in Tests

### Task 1: Add failing regression tests

**Files:**
- Modify: `cli/clients_test.go`
- Modify: `tests/onboarding/install-skills-per-client.test.ts`
- Reference: `docs/superpowers/specs/2026-03-16-claude-current-cache-layout-design.md`

- [ ] Add a Go test for current direct-root Claude cache layout
- [ ] Add a shell-contract test for current direct-root Claude cache layout
- [ ] Keep legacy nested-layout coverage intact
- [ ] Run focused tests and watch the new coverage fail first

## Chunk 2: Implement the Smallest Cleanup Fix

### Task 2: Normalize Claude cache cleanup

**Files:**
- Modify: `cli/clients.go`
- Modify: `cli/claude_marketplace.go`
- Modify: `scripts/onboarding/install-local-skills.sh`

- [ ] Clear `~/.claude/plugins/cache/ha-nova` in local validation cleanup
- [ ] In the Go installed-bundle local-override path, stage Claude marketplace payloads under `~/.config/ha-nova/claude-marketplace/ha-nova`
- [ ] Exclude the top-level bundled `ha-nova` binary from those installed-bundle payloads
- [ ] Keep behavior idempotent when the cache is already absent
- [ ] Avoid changing end-user GitHub marketplace behavior

## Chunk 3: Verify + Record

### Task 3: Verify and document

**Files:**
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Run focused Claude tests
- [ ] Run `cd cli && go test ./...`
- [ ] Run `npm run verify`
- [ ] Record the new cache-layout rule and verification evidence
