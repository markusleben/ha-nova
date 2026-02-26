---
name: ha-nova
description: Strict orchestrator for Home Assistant operations through App + Relay with mandatory onboarding-health gate and deterministic skill routing.
---

# HA NOVA Orchestrator

## Mission

Route user requests to the correct HA NOVA skill path with strict preconditions and no ambiguous execution.

Primary model:
- App + Relay first.
- Direct HA REST only when required and authorized.
- Writes always require preview + explicit confirmation.

## Mandatory Gate (Run Before Any HA Operation)

1. Run onboarding health check:
   - `bash scripts/onboarding/macos-onboarding.sh doctor`
2. If any required check fails:
   - stop operation routing,
   - route to `ha-onboarding`,
   - continue only after `doctor` is healthy.
3. Resolve capability mode for this session:
   - `app_relay_only`: `HA_URL` + `RELAY_BASE_URL` + `RELAY_AUTH_TOKEN` available, no `HA_LLAT`
   - `direct_rest_enabled`: same inputs plus `HA_LLAT`

## Session Inputs

- Required:
  - `HA_URL`
  - `RELAY_BASE_URL`
  - `RELAY_AUTH_TOKEN`
- Optional:
  - `HA_LLAT` (enables full direct REST flows)

## Active Skill Catalog

- `ha-onboarding`: setup and connectivity recovery
- `ha-safety`: write safety baseline
- `ha-entities`: entity discovery
- `ha-control`: service-based device control
- `ha-automation-crud`: automation config lifecycle
- `ha-automation-control`: runtime automation enable/disable/toggle

## Deterministic Routing Rules

1. Setup, auth, connectivity, onboarding failures -> `ha-onboarding`
2. Read/search/list entities -> `ha-entities`
3. Device control (service calls) -> `ha-control` + `ha-safety` for writes
4. Automation create/read/update/delete -> `ha-automation-crud` + `ha-safety`
5. Automation enable/disable/toggle -> `ha-automation-control` + `ha-safety`

## Capability Routing Rules

- In `app_relay_only` mode:
  - allow App + Relay operations.
  - for direct REST flows that require LLAT, stop and route to `ha-onboarding`.
- In `direct_rest_enabled` mode:
  - allow all active skill flows.

## Safety Baseline

- Never guess entity IDs or service names.
- Resolve ambiguity by listing/filtering first.
- Show preview before write operations.
- Wait for explicit user confirmation before execute.
