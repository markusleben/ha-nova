# Setup Wizard Parity Design

## Goal

Restore the new Go-based `ha-nova setup` flow to at least the UX quality of the current release wizard, while keeping the Go runtime and cross-platform installer architecture.

## Scope

- Interactive `ha-nova setup` UX only
- Windows and macOS userflow parity
- Keep current Go runtime ownership for setup/install/update/uninstall
- Align client choices with proven platform support

## Non-Goals

- No return to the old shell runtime
- No new GUI framework
- No extra “enterprise” wizard abstraction layer
- No broad refactor outside setup UX and its supporting checks

## Current Gap

The Go flow currently only restored the first client-selection prompt. The rest of the old release experience is still missing:

- no header / step framing
- no phased setup guidance
- no “already done” / resume summary
- no guided relay/HA verification loop
- no success / incomplete banner parity

This is a real UX regression versus the current release.

## Platform Support Inputs

### Windows

- `Claude Code`: native Windows documented by Anthropic
- `Gemini CLI`: Windows supported
- `OpenCode`: Windows documented, but WSL-first
- `Codex`: Windows exists, but support wording must stay conservative until fully proven

Design consequence:

- keep the old 5-option list for parity
- mark unsupported/WSL-first cases in wording later if needed
- do not remove options from the interactive list unless product support is explicitly dropped

## Design

### 1. Restore a real setup wizard in Go

Add a small Go-native setup UI layer that mirrors the old shell wizard behavior:

- header
- numbered client selection
- explicit phases
- success/fail/info lines
- short pause/continue prompts where needed

This should be a tiny setup-specific UI helper, not a generic terminal UI framework.

### 2. Keep the old release’s four-phase mental model

The Go flow should map to the old release structure:

1. Install NOVA Relay / prerequisites guidance
2. Set up secure access
3. Verify connection
4. Install HA NOVA skills

Windows and macOS can differ in platform-specific actions, but the overall flow and wording should feel the same.

### 3. Restore resume/status behavior

Before running setup actions, detect current state and show a compact status block:

- relay reachable
- auth valid
- websocket connected
- skills installed

If everything is already complete:

- show the success state
- exit cleanly

If some phases are already done:

- skip them
- show “already done” summary

### 4. Restore guided verification and incomplete exits

Do not hard-fail immediately on first relay/WS problem in interactive mode.

Interactive flow should:

- explain what failed
- give a short checklist
- allow retry
- allow finishing as incomplete when appropriate

Non-interactive mode may still fail fast.

### 5. Keep release-safe boundaries

Do not reintroduce host-risky defaults:

- browser opening still respects `HA_NOVA_NO_BROWSER`
- safe tests remain safe
- desktop validation remains explicit

## File Structure

- `cli/commands.go`
  - keep orchestration, but move setup-UI-heavy pieces out
- `cli/setup_ui.go`
  - setup-specific prompts, headers, step display, banners
- `cli/setup_state.go`
  - setup state detection / resume summary
- `cli/setup_ui_test.go`
  - prompt and banner tests
- `cli/setup_state_test.go`
  - resume / state detection tests
- `tests/onboarding/desktop-validation-contract.test.ts`
  - ensure desktop validation docs/scripts still point at the Go setup path
- `tests/onboarding/windows-installer-contract.test.ts`
  - ensure installer still launches setup directly

## Acceptance Criteria

- Interactive `ha-nova setup` shows at least the old release’s client list UX
- Setup shows header + phased guidance instead of a bare prompt stream
- Interactive users get guided retry/incomplete behavior for relay/WS issues
- Resume/status summary exists before phase execution
- Success/incomplete ending is explicit and user-friendly
- Safe test system remains intact
- Windows and macOS desktop validation still pass
