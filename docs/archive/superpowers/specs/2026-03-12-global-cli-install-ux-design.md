# Global CLI Install UX

Date: 2026-03-12

## Goal

Make the post-install command path consistent and usable from any terminal directory.

## Problem

- The one-line installer creates `~/.local/bin/ha-nova`.
- User-facing onboarding and docs still mention `npx ha-nova ...`.
- `npx ha-nova` fails for normal installer users because there is no published npm package.
- If `~/.local/bin` is missing from `PATH`, the installed CLI may still fail in the current shell right after setup.

## Decision

- Standard post-install commands use `ha-nova ...`.
- `npx ha-nova ...` stays limited to explicit repo-local/development contexts.
- The installer persists `~/.local/bin` into the detected shell startup file.
- After setup, the installer prints a direct fallback command for the current shell if that shell still has the old `PATH`.

## Scope

- `install.sh`
- `scripts/onboarding/macos-lib.sh`
- top-level README guidance
- client install docs in `./.codex`, `./.claude`, `./.opencode`, `./.gemini`

## Non-Goals

- Publishing `ha-nova` to npm
- Changing repo-internal development workflows beyond explicit repo-local guidance
