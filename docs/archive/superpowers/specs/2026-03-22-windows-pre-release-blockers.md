# Spec: Windows Pre-Release Blockers

Date: 2026-03-22

## Goal

Remove the last Windows release blockers before any RC/final publish:

- prevent `install.ps1` from creating mixed `bundle + winget` installs
- stop stale `state.json` from overriding live install-source detection
- stop stale `WinGet/Packages/*` remnants from being treated as a live winget install
- keep post-`winget` update client sync anchored to the active winget runtime
- document the private Windows RC matrix, with explicit clean-update and Claude cache regression checks

## Constraints

- UX first: clear failure beats ambiguous auto-repair
- one owner per concern: installer handles placement, Go handles product lifecycle
- no new permanent compatibility layer for stale Windows remnants

## Implementation

1. `install.ps1`
   - detect an existing winget-managed HA NOVA install
   - abort before bundle install with a one-path-only message

2. `cli/install_source.go`
   - prefer live runtime markers over persisted `install_source`
   - treat persisted `install_source` as a weak fallback only

3. `cli/install_channels.go`
   - use the live winget link as the presence signal
   - resolve the winget bundle root from the live alias target when possible
   - never guess from stale package remnants alone

4. `cli/command_update.go`
   - run post-winget-update sync via the resolved active runtime, not generic PATH lookup

5. Tests + docs
   - add regression coverage for stale state, stale package remnants, and installer mixed-channel refusal
   - extend release docs with a private Windows RC matrix for install/update/uninstall plus Claude cache regression
