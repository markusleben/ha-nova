# Gemini Skill Root Isolation

Date: 2026-03-12

## Goal

Eliminate duplicate HA NOVA skill discovery in Codex when Gemini is also installed.

## Problem

- Codex currently uses `~/.agents/skills/ha-nova -> <repo>/skills`.
- Gemini flat copies were also installed into `~/.agents/skills/ha-nova-*`.
- Codex then discovers the same logical skills twice: once through the Codex symlink tree and once through the Gemini flat copies.

## External Research

- Gemini Agent Skills docs define client-owned skill tiers and support both `.gemini/skills` and `.agents/skills`, with precedence rules inside each tier.
- Gemini also exposes native `skills link` / `skills install` commands, which suggests Gemini should own its own user scope instead of sharing duplicate artifacts in another client's root.
- XDG base directory guidance favors app-owned user directories with ordered lookup, not duplicate installations of the same logical capability in one shared discovery root.

## Decision

- Keep Codex on `~/.agents/skills/ha-nova`.
- Move Gemini flat copies to `~/.gemini/skills/ha-nova*`.
- Clean up legacy Gemini flat copies from `~/.agents/skills` during Gemini install.
- Update self-update, dev-sync, readiness detection, uninstall, and docs to the isolated Gemini root.

## Non-Goals

- Rework Codex skill discovery itself
- Change Claude or OpenCode install roots
