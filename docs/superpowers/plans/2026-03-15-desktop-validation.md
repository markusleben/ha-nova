# Desktop Validation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the new private RC installer and setup flow end-to-end on macOS and Windows, with a real Windows desktop-session lane for client validation.

**Architecture:** Freeze one private RC artifact set, then run four explicit validation lanes: artifact integrity, macOS fresh-home, Windows headless installer, and Windows desktop setup/client validation. Keep Windows desktop proof separate from SSH/headless proof so Credential Manager and real client availability are measured honestly.

**Tech Stack:** Bash, PowerShell, Python HTTP mock server, SSH, RDP, Vitest, Go test

---

## Chunk 1: Freeze Artifacts and Validation Harness

### Task 1: Lock the artifact rehearsal contract

**Files:**
- Modify: `docs/releasing.md`
- Modify: `docs/choices.md`
- Modify: `docs/breadcrumbs.md`

- [ ] Add one explicit precondition block for desktop validation: `npm run verify` then `npm run release:rc:local`
- [ ] State clearly that bundle-only rebuilds are invalid for validation because they can reuse stale `dist/` binaries
- [ ] Document that no validation in this plan is allowed against `main` or a public release

### Task 2: Add a small local mock-server helper

**Files:**
- Create: `scripts/dev/mock-ha-relay.py`
- Test: manual invocation only

- [ ] Add a tiny Python script that serves:
  - `GET /` on `:8123` returning `200 OK`
  - `GET /health` on `:8791` returning JSON with `status` and `version`
- [ ] Keep the script intentionally tiny and dependency-free
- [ ] Print the listening ports on startup

### Task 3: Add a reusable Windows cleanup helper

**Files:**
- Create: `scripts/dev/windows-clean-test-state.ps1`
- Modify: `docs/releasing.md`

- [ ] Remove current install/config/legacy bin paths from the Windows test user
- [ ] Remove only HA NOVA-owned test client paths:
  - `%USERPROFILE%\.agents\skills\ha-nova`
  - `%USERPROFILE%\.config\opencode\skills\ha-nova`
  - `%USERPROFILE%\.gemini\skills\ha-nova*`
  - `%USERPROFILE%\.claude\skills\ha-nova`
- [ ] Remove old test `PATH` entries for the HA NOVA install root
- [ ] Document this script as the first step for every Windows validation lane

## Chunk 2: macOS Validation Lane

### Task 4: Add a macOS private-RC runner

**Files:**
- Create: `scripts/dev/macos-private-rc-smoke.sh`
- Modify: `docs/releasing.md`

- [ ] Use a fresh temp `HOME`
- [ ] Start with installer override env vars:
  - `HA_NOVA_BUNDLE_URL`
  - `HA_NOVA_BUNDLE_SHA256_URL`
  - `HA_NOVA_NO_SETUP=1`
- [ ] Run:
  - install
  - `ha-nova version`
  - `ha-nova uninstall --yes`
- [ ] Fail hard on any missing expected path transition

### Task 5: Add a macOS setup-all runner

**Files:**
- Create: `scripts/dev/macos-private-rc-setup-all.sh`
- Modify: `docs/releasing.md`

- [ ] Use a fresh temp `HOME`
- [ ] Start the local mock HA/Relay server
- [ ] Run:
  - install
  - `ha-nova setup all --host 127.0.0.1 --relay-token test-relay-token --non-interactive`
  - `ha-nova doctor`
  - `ha-nova relay version`
  - `ha-nova update --version <same-version>`
  - `ha-nova uninstall --yes`
- [ ] Capture per-step output to a log file

### Task 6: Add macOS per-client runners

**Files:**
- Create: `scripts/dev/macos-private-rc-client.sh`
- Modify: `docs/releasing.md`

- [ ] Accept a single client argument: `claude|codex|opencode|gemini`
- [ ] Use a fresh temp `HOME` per run
- [ ] Run install + `ha-nova setup <client>` + `ha-nova uninstall --yes`
- [ ] Check expected artifacts:
  - `codex`: `~/.agents/skills/ha-nova/...`
  - `opencode`: `~/.config/opencode/skills/ha-nova/...`
  - `gemini`: `~/.gemini/skills/ha-nova...`
  - `claude`: plugin registration attempt or explicit skip/warn path

## Chunk 3: Windows Validation Lanes

### Task 7: Add a Windows headless installer runner

**Files:**
- Create: `scripts/dev/windows-private-rc-install.ps1`
- Modify: `docs/releasing.md`

- [ ] Run only:
  - cleanup
  - install with bundle override
  - `ha-nova version`
  - `ha-nova uninstall --yes`
- [ ] Poll for install-root disappearance after uninstall
- [ ] Write results to a stable result file
- [ ] Document explicitly: this lane does not prove `setup`

### Task 8: Add a Windows desktop setup runner

**Files:**
- Create: `scripts/dev/windows-desktop-setup.ps1`
- Modify: `docs/releasing.md`

- [ ] Accept a client argument
- [ ] Assume execution inside an interactive desktop session
- [ ] Run:
  - cleanup
  - install with bundle override
  - `ha-nova setup <client> --host <host> --relay-token <token> --non-interactive`
  - `ha-nova doctor`
  - same-version `ha-nova update`
  - `ha-nova uninstall --yes`
- [ ] Write all output to `%USERPROFILE%\ha-nova-desktop-validation.txt`
- [ ] Capture explicit checks for:
  - token save success
  - client skill/plugin artifact creation
  - same-version update success
  - uninstall cleanup

### Task 9: Define the Windows client support matrix

**Files:**
- Modify: `docs/releasing.md`
- Modify: `.codex/INSTALL.md`
- Modify: `.claude/INSTALL.md`
- Modify: `.gemini/INSTALL.md`
- Modify: `.opencode/INSTALL.md`

- [ ] Add one matrix table with columns:
  - client
  - macOS support
  - Windows native support
  - Windows WSL support
  - proof status
- [ ] Mark unsupported/unproven Windows clients explicitly
- [ ] Do not claim native Windows support for any client that has not passed Task 8

## Chunk 4: Verification and Review

### Task 10: Verify automated checks

**Files:**
- Test: `tests/onboarding/installer-contract.test.ts`
- Test: `tests/onboarding/windows-installer-contract.test.ts`
- Test: `tests/onboarding/release-contract.test.ts`
- Test: `cli/uninstall_helper_test.go`

- [ ] Run `npm run verify`
- [ ] Run `cd cli && go test ./...`
- [ ] Re-run targeted contract tests if any docs/contracts changed

### Task 11: Run the real validation matrix

**Files:**
- Test: manual + scripted lane execution
- Modify: `docs/breadcrumbs.md`

- [ ] Run macOS private RC smoke
- [ ] Run macOS setup-all lane
- [ ] Run macOS per-client lanes
- [ ] Run Windows headless installer lane
- [ ] Run Windows desktop lane for each candidate client
- [ ] Record pass/fail and root cause for each lane in `docs/breadcrumbs.md`

### Task 12: Final review gate

**Files:**
- Modify: `docs/releasing.md`
- Modify: `docs/choices.md`

- [ ] Review every validation result against the support matrix
- [ ] Remove any support claim not proven by the executed lanes
- [ ] Add explicit release blockers for remaining red lanes
- [ ] Request code review before any merge/release decision
