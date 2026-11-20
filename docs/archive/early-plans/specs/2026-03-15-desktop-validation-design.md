# Desktop Validation Design

## Goal

Define the smallest reliable validation flow that proves HA NOVA's new installer, setup, client integration, update, and uninstall behavior on macOS and Windows before anything merges to `main` or ships publicly.

## Scope

- Validate the private RC path only; no public release and no merge to `main`.
- Cover installer, setup, doctor, per-client integration, update, and uninstall.
- Distinguish headless Windows checks from true desktop-session checks.
- Produce a repeatable maintainer runbook and support matrix inputs.

## Non-Goals

- No new public release workflow.
- No GUI automation framework.
- No unsupported-client promises for Windows before proof.

## Approaches Considered

### 1. SSH + RDP hybrid

Prepare the VM and artifacts over SSH, execute the interactive validation in a real RDP desktop session, then collect logs over SSH.

Pros:
- Real Windows desktop logon context
- Reproducible
- Minimal new tooling

Cons:
- Still needs a human-visible desktop session

### 2. Fully automated GUI control over RDP

Drive the full desktop remotely through UI automation.

Pros:
- Maximum automation

Cons:
- High complexity
- Fragile in the current harness
- Not needed for current confidence goals

### 3. Manual desktop testing without preparation

Run everything by hand inside macOS and Windows sessions.

Pros:
- Low setup

Cons:
- Weak evidence
- Hard to reproduce
- Easy to miss cleanup/state drift

## Recommended Design

Use a strict hybrid validation model:

- macOS: local fresh-home validation with private RC bundle overrides
- Windows headless: installer/version/uninstall only
- Windows desktop: setup/client validation only through a real desktop logon session

The system under test must always be built from fresh private RC artifacts:

1. `npm run verify`
2. `npm run release:rc:local`

`npm run release:rc:local` is mandatory because bundle archives consume the current `dist/` binaries; rebuilding only install bundles can silently reuse stale executables.

## Test Lanes

### Lane A: Artifact Integrity

Required on the maintainer machine before any platform test:

- `npm run verify`
- `npm run release:rc:local`
- confirm expected bundle files exist for macOS and Windows

### Lane B: macOS Fresh-Home

Use private bundle override env vars with a fresh temporary `HOME`.

Required checks:
- install
- `ha-nova version`
- `ha-nova setup all --host ... --relay-token ... --non-interactive`
- `ha-nova doctor`
- `ha-nova relay version`
- same-version `ha-nova update`
- `ha-nova uninstall --yes`

Then run per-client fresh-home passes:
- `claude`
- `codex`
- `opencode`
- `gemini`

### Lane C: Windows Headless

Use SSH only for:
- cleanup
- private installer execution
- `ha-nova version`
- `ha-nova uninstall --yes`

Do not use SSH/headless as proof for `setup` because Windows Credential Manager can fail without a suitable interactive logon session.

### Lane D: Windows Desktop

Use a real RDP desktop session for:
- `ha-nova setup <client>`
- `ha-nova doctor`
- per-client path/plugin checks
- same-version `ha-nova update`
- `ha-nova uninstall --yes`

The desktop session should execute a prepared in-VM test runner so the interaction stays short and the results are written to a known log file.

## Windows Support Matrix Rule

Do not promise a Windows client publicly until it passes the desktop lane.

Temporary validation order:
- `claude`: first desktop candidate
- `gemini`: second desktop candidate
- `codex`: verify native Windows vs WSL expectation before public support
- `opencode`: treat as WSL candidate unless native Windows proof is explicit

## Evidence Requirements

Each lane must leave artifacts:

- command transcript or captured log
- exit status
- installed path/plugin evidence
- uninstall/cleanup evidence

Windows desktop validation must explicitly capture:
- whether token save succeeded
- whether file-based client skill trees were created
- whether Claude plugin registration ran or was skipped

## Definition of Done

This validation design is complete when:

- macOS fresh-home core lane passes
- macOS per-client passes complete
- Windows headless installer lane passes
- Windows desktop setup lane passes for each officially supported Windows client
- docs and support wording match the proven matrix only
