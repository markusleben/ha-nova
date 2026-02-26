# Fresh Session MVP Readiness Design

Date: 2026-02-26

## Goal

Enable a fresh Codex session to quickly verify HA NOVA skill readiness with minimal friction:
- fast default check for daily use
- heavy CRUD smoke kept optional for contributor deep validation

## Scope

In scope:
- add `quick` command to `scripts/onboarding/macos-onboarding.sh`
- implement `run_quick` in `scripts/onboarding/macos-lib.sh`
- add npm shortcut for quick MVP check
- document quick-vs-heavy workflow
- add/update contract tests

Out of scope:
- marketplace packaging
- non-macOS onboarding
- new fallback layers/backward compatibility paths

## Behavior

Quick check (`quick`) must:
1. run existing doctor checks (HA + Relay + token)
2. confirm local HA NOVA skill file exists for Codex (`~/.agents/skills/ha-nova/SKILL.md`)
3. verify skill references this repository path marker (`ha-nova-managed-install repo_root:`)
4. print exactly what to run next in a fresh Codex session

Failure policy:
- fail loud with actionable single-step fix
- no silent fallback behavior

## KISS/MVP Notes

- default path for users/contributors: `quick`
- optional deep path for contributors only: `smoke:app:mvp`
- keep App + Relay terminology everywhere
- keep project docs/scripts English-only
