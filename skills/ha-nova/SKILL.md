---
name: ha-nova
description: Use when the user wants Home Assistant operations through HA NOVA (App + Relay) with macOS Keychain-backed local auth.
---

# HA NOVA Context Skill

This context is auto-loaded via SessionStart hook. Sub-skills are discovered independently by Claude Code via their descriptions.

## Mission

Operate Home Assistant through HA NOVA with a minimal user-facing flow:
- App + Relay first
- preview before write
- one blocking question only when required
- compact result output

## Runtime Prerequisite (macOS)

Before HA operations in this session:

1. Verify relay CLI: `~/.config/ha-nova/relay health`
2. If this fails, ask user to run: `npm run onboarding:macos`
3. Do not run diagnostics proactively; diagnose only after real failure.
4. Relay-only auth model: do not request or persist LLAT client-side.
   - LLAT belongs in App option `ha_llat`

Do not ask user to paste tokens in chat.

## Self-Update

If the SessionStart context includes `UPDATE AVAILABLE`, inform the user and offer to update:
1. Run: `~/.config/ha-nova/update`
2. If the script doesn't exist (older install): run `bash scripts/update.sh` from the repo root, or tell the user to re-run `ha-nova setup`.
3. After success: tell the user to **start a new session** for the updated skills to take effect.

## Quoting Reliability (Critical)

Quoting is shell-dependent (bash/zsh vs PowerShell), not primarily OS-dependent.

Rules:
- Keep command examples copy-pastable as shown.
- Avoid unnecessary escaping in bash/zsh snippets.
- If shell is not bash-compatible, stop and ask user to switch shell.

## Safety Baseline

- Never guess entity IDs, service names, or config IDs.
- Preview every write payload.
- Confirmation tiers:
  - `create`/`update`: natural confirmation bound to active preview.
  - `delete`/destructive: token confirmation `confirm:<token>`.
    **Strict token enforcement:** User MUST reply with the exact token string (e.g., `confirm:del-kitchen-lights`). Any other response — including "yes", "ja löschen", "do it", or any natural-language confirmation — is NOT valid. Reject and re-prompt: "Bitte den exakten Token eingeben: `confirm:<token>`"
- Ask exactly one blocking question only if ambiguity remains.
- Failure format must include:
  - what failed
  - why it failed
  - next concrete step

## Claim-Evidence Binding (Critical)

Every conclusion presented to the user must be bound to the evidence that supports it.

Before presenting any conclusion, verify:
1. **Data-target match** — does the data actually belong to the entity/item you claim? Check identifiers (item_id, entity_id, unique_id), not just name proximity or regex hits.
2. **Completeness** — full relevant data, or partial/truncated subset?
3. **Recency** — current data, or potentially stale?

Confidence tiers in output:
- **Verified** (default, no marker needed) — data retrieved, identifier confirmed, conclusion follows.
- **Likely** (mark: "Based on [evidence], this likely means...") — strong indirect evidence, no direct confirmation available.
- **Uncertain** (mark: "Could not verify [X]. Found: [evidence]. Manual check recommended.") — ambiguous, incomplete, or multi-match data.

Rules:
- Never present "likely" or "uncertain" in the same tone as "verified."
- If verification exhausted and still uncertain, say so. No gap-filling with assumptions.
- Wrong confident answer is worse than honest "I could not determine this."

## Response Format

Render structured summary + YAML for both reads and writes:
1. `Automation` or `Script` (name + ID)
2. `Entities` (all entity_ids in triggers/conditions/actions)
3. Domain-specific fields:
   - **Automation:** `Triggers`, `Conditions`, `Actions` (short descriptions)
   - **Script:** `Fields` (input parameters, if present), `Sequence` (short description of steps)
   - **Helper:** `name` (type + entity_id), type-specific fields (min/max, options, duration, etc.)
4. `Mode` (single/restart/queued/parallel) — automations/scripts only
5. full YAML config block (or WS payload for helpers)
6. `Next Step` (for writes: confirmation; for reads: done)

Keep orchestration details internal on normal success paths.

## Skill Dispatch (Critical)

**Always invoke exactly ONE ha-nova skill per user intent.** Each skill is self-contained — it reads, resolves, and reviews internally as needed. Never load two ha-nova skills in parallel.

Match user intent to exactly one skill:

| User wants to… | Invoke exactly |
|---|---|
| list, show, read automations/scripts | `ha-nova:read` |
| analyze, review, audit, check, find problems | `ha-nova:review` (reads config internally) |
| create, update, delete automations/scripts | `ha-nova:write` (resolves + reviews internally) |
| list, show, read helpers | `ha-nova:helper` |
| create, update, delete helpers | `ha-nova:helper` |
| turn on/off, toggle, set, call a service | `ha-nova:service-call` |
| enable/disable/trigger an automation | `ha-nova:service-call` |
| find entities by name, room, area | `ha-nova:entity-discovery` |
| fix relay/auth/connectivity errors | `ha-nova:onboarding` |
| HA-related but not matched above (dashboards, blueprints, history, energy, areas, zones, etc.) | `ha-nova:guide` |

**"Analysiere meine Automation"** → `ha-nova:review` (NOT read + review)
**"Zeige meine Automationen"** → `ha-nova:read` (NOT review)
**"Erstelle eine Automation"** → `ha-nova:write` (NOT read + write)
**"Erstelle einen input_boolean"** → `ha-nova:helper` (NOT write)
**"Zeige meine Helper"** → `ha-nova:helper` (NOT read)
**"Erstelle einen Timer"** → ambiguous! Ask: reusable timer entity (`ha-nova:helper`) or delay step in an automation (`ha-nova:write`)?
**"Zeige mir mein Energy Dashboard"** → `ha-nova:guide` (no dedicated skill)
**"Importiere einen Blueprint"** → `ha-nova:guide` (relay-ready, no skill)
**"Wie manage ich Add-ons?"** → `ha-nova:guide` (external, web search)
**"Zeige mir die History von Sensor X"** → `ha-nova:guide` (relay-ready, no skill)

## Latency Policy

- Prefer one-shot reads over multi-step probing.
- For first read/list, try Relay `/ws` directly.
- For write flows, keep main-thread file reads minimal:
  - context skill (this file)
  - `skills/ha-nova/relay-api.md`
  - one agent template per phase
- No proactive doctor in success path.
- Re-read full state snapshot only with explicit reason.
