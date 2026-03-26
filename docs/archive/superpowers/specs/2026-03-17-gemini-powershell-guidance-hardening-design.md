# Gemini PowerShell Guidance Hardening

Date: 2026-03-17

## Problem

Gemini on Windows successfully used HA NOVA, but it still made avoidable shell mistakes:

- chained commands with `&&` under Windows PowerShell
- attempted complex inline `--jq` filters instead of file-based filters
- fell back to external `jq`, which is not part of the HA NOVA contract
- expanded a simple `light` count into extra `switch` heuristics without being asked

## Goal

Reduce Gemini freedom on Windows/PowerShell for HA NOVA tasks:

- explicit no-`&&` rule
- explicit no-external-`jq` rule
- stronger `--jq-file` default for non-trivial and count filters
- simpler domain-count behavior

## Decision

Harden the shared HA NOVA context skill plus `entity-discovery`:

- Windows PowerShell: never use `&&` or `||`
- use separate shell calls instead
- never call external `jq`
- use `ha-nova relay jq` or relay-native `--jq-file`
- domain counts stay domain-only unless the user explicitly asks for heuristic expansion

## Non-Goals

- no runtime shell wrapper changes
- no Gemini-specific prompt injection outside the skill files
