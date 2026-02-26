# Phase 1a.5 Fast Deploy Loop Design

Date: 2026-02-26
Status: approved-for-implementation

## Goal

Provide a contributor-friendly deploy loop for Home Assistant App development that makes code changes visible quickly and handles aggressive cache behavior.

## Problem

Manual UI-driven update paths are slow and unreliable for iteration:
- app store metadata can lag behind (`Check for updates` + browser cache)
- image/layer caches can keep old code visible
- contributors need a single repeatable command, not many manual clicks

## Approach

Implement one SSH-based deploy helper that runs Home Assistant CLI commands on the HA host.

Modes:
- `fast` (default): store reload + ensure installed + rebuild + start
- `clean`: all from `fast` + stop app + remove app image cache before rebuild

## Why SSH + HA CLI

- Works with current HA Supervisor flows (`ha apps ...`)
- Deterministic for local repositories and active development
- Avoids brittle frontend cache dependency

## Interface

Command:
- `bash scripts/deploy/ha-app-deploy.sh --mode fast|clean`

Required env:
- `HA_HOST`
- `HA_SSH_KEY`

Optional env:
- `SSH_USER` (default `root`)
- `SSH_PORT` (default `22`)
- `APP_SLUG` (default `ha_nova_bridge`)
- `SUPERVISOR_SLUG` (default `local_${APP_SLUG}`)

## Success Criteria

- single command performs deploy workflow without manual UI steps
- `clean` mode removes app image cache and forces rebuild path
- script fails loudly with clear action hints
- contributor docs include copy/paste quickstart and cache troubleshooting
