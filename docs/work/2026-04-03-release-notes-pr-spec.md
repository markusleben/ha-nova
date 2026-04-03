# Release Notes PR Spec

Date: 2026-04-03

## Goal

Open and merge the current review/write UX + proof-hardening work with a release-note framing that stays plain, short, and end-user focused.

## Decisions

- Keep the user-facing release notes in plain English.
- Prefer outcomes over implementation details.
- Keep the review/write improvements visible as their own user-facing benefit.
- Do not surface harness-only or QA-only hardening as a public release-note bullet.

## Intended PR Release Notes

### New Features

- Windows now has one clear install path: `install.ps1`. The old WinGet path has been removed.
- Claude installs now match the HA NOVA version you installed more reliably.
- HA NOVA now catches more automation issues when creating or editing automations.
- Review and write feedback is now easier to understand, with clearer warnings and practical suggestions instead of internal rule codes.
- Setup guidance for Claude, Codex, Gemini, and OpenCode is now simpler and easier to follow.

### Bug Fixes

- Leftover files from older Windows installs are cleaned up more reliably so they do not interfere with new installs.
