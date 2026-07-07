# Antigravity Desktop Support Spec

## Problem

The Antigravity migration described the client as "Google Antigravity CLI" and detected readiness only through the `agy` command. That incorrectly marks a desktop-only Antigravity install as not ready, even though the shared Antigravity skill root is present.

## MVP Fix

- Treat `antigravity` as Google Antigravity, not CLI-only.
- Keep one client id and one shared skill layout: `~/.gemini/config/skills/ha-nova-*`.
- Detect runtime availability when either:
  - `agy` is available, or
  - Antigravity desktop profile/app markers are present.
- Keep `agy --version` only as a CLI-specific verification hint.

## Verification

- Go tests for runtime detection and setup/status labels.
- Docs contract tests for README and `.antigravity/INSTALL.md`.
- Local status check on a desktop-only Mac.
