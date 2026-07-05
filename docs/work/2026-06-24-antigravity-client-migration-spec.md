# Spec: Google Antigravity Client Migration

Date: 2026-06-24
Status: active

## Context

Google announced the transition from Gemini CLI to Antigravity for individual/free/Pro/Ultra users. HA NOVA should not ship a new release that still presents Gemini CLI as the current Google client.

## Scope

- Replace the public Gemini client surface with Google Antigravity.
- Install Antigravity flat skills under `~/.gemini/config/skills/ha-nova-*`.
- Keep `gemini` as a legacy setup/install alias that resolves to Antigravity.
- Clean HA NOVA-owned legacy Gemini flat skills from `~/.gemini/skills/ha-nova*` during Antigravity install/uninstall.
- Update README, client overlays, reference docs, dev validation helpers, and tests.

## Non-Goals

- No release/tag/publish in this branch.
- No support for arbitrary Antigravity plugin systems beyond the documented flat skill folder.
- No migration of user-owned non-HA-NOVA Gemini skills.

## Verification

- Go tests for client install/status/setup/state migration.
- Onboarding/docs Vitest contracts for Antigravity docs, installer, dev-sync, and validation helpers.
- `npm run verify:docs`.
- Before PR: show status, diff stat, public claim diffs, targeted tests, and risks.
