---
name: ha-nova
description: Use when the user wants Home Assistant operations through HA NOVA (App + Relay) with local OS-backed auth.
---

# HA NOVA Context Skill

This context is auto-loaded via SessionStart hook. Sub-skills are discovered independently by Claude Code via their descriptions.

## Mission

Operate Home Assistant through HA NOVA with a minimal user-facing flow:
- App + Relay first
- preview before write
- one blocking question only when required
- compact result output
- when scope exceeds one target, scale manually with the same rules and report the exact audited subset

## Runtime Prerequisite

Before HA operations in this session:

1. Verify relay CLI: `ha-nova relay health`
2. If this fails, ask user to run: `ha-nova setup`
3. Do not run diagnostics proactively; diagnose only after real failure.
4. Relay-only auth model: do not request or persist LLAT client-side.
   - LLAT belongs in App option `ha_llat`

Do not ask user to paste tokens in chat.

## Self-Update

Before the first HA task in a session:
1. If session context already contains HA NOVA update status, use it.
2. Otherwise run: `ha-nova check-update --quiet`
3. If the output contains `UPDATE AVAILABLE`, inform the user and offer to update.
4. If the output is empty, continue silently.

When an update is available:
1. Run: `ha-nova update`
2. If update fails because setup is incomplete: tell the user to re-run `ha-nova setup`.
3. After success: tell the user to **start a new session** for the updated skills to take effect.

## Quoting Reliability (Critical)

Quoting is shell-dependent (bash/zsh vs PowerShell), not primarily OS-dependent.

Rules:
- The canonical relay contract is file-based, not inline-JSON-first.
- Prefer `ha-nova relay ws --data-file <payload-file>`.
- Prefer `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`.
- Prefer `ha-nova relay ... --out <result-file>` for large outputs.
- Prefer `--jq` or `--jq-file` over shell pipes when filtering relay output.
- Prefer `ha-nova relay jq --file <result-file> length` for simple counts and `--jq-file <filter-file>` for non-trivial follow-up transforms.
- On Windows PowerShell, never chain commands with `&&` or `||`; run separate shell commands instead.
- Never call external `jq`; use relay-native `--jq` / `--jq-file` or `ha-nova relay jq`.
- When a filter contains `select`, `test`, `startswith`, or more than one pipeline stage, default to `--jq-file` even if inline quoting might work.
- Use native file-writing and file-reading tools for temp files. Do not teach `cat`, heredocs, Python, or Node as the primary JSON path.
- Use inline `-d` / `--body` only for tiny diagnostics when shell quoting is already known-good.

## Safety Baseline

- Never guess entity IDs, service names, or config IDs.
- Correct invalid Home Assistant premises explicitly.
- Do it briefly and technically.
- Preview every write payload.
- Confirmation tiers:
  - `create`/`update`: natural confirmation bound to active preview.
  - `delete`/destructive: token confirmation `confirm:<token>`.
    **Strict token enforcement:** User MUST reply with the exact token string (e.g., `confirm:del-kitchen-lights`). Any other response — including "yes", "sure, delete it", "do it", or any natural-language confirmation — is NOT valid. Reject and re-prompt with the exact token required.
- Ask exactly one blocking question only if ambiguity remains.
- **No raw relay writes without a skill**: If no dedicated subskill matches, you MUST invoke `ha-nova:fallback` before any raw `relay ws` or `relay core` write operation. Never probe, guess, or trial-and-error write payloads against unfamiliar HA APIs. Some WS endpoints (e.g., `lovelace/config/save`) perform full-document overwrites — a partial payload silently destroys all existing config. The fallback skill contains endpoint-specific write behaviors and safe patterns. Skipping it risks data loss.
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

## Output Localization (Critical)

All user-facing output MUST follow these rules:
- **Language**: Localize all section headings and labels to the user's language. Use idiomatic phrasing, not literal translations.
- **Severity**: 3 levels only — 🔴 (high/critical) 🟠 (medium) 🟡 (low/info). No text severity labels needed — the emoji is sufficient.
- **Finding titles**: Each finding gets a short descriptive title (2-5 words) explaining WHAT the issue is. Example: "Missing template fallback", not "R-01". Localize at runtime.
- **Internal codes**: Check codes (R-01, S-01, H-01, M-01, P-01, F-01, etc.) are for YOUR analysis reference only. NEVER show them in user-facing output.
- **Consistency**: Same sections, same order, every time. The user must recognize the structure across reviews.

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
| **any HA task not matched above** — dashboards, blueprints, history, energy, areas, zones, any raw relay/ws/core write | `ha-nova:fallback` **(mandatory fallback — never skip)** |

**"Analyze my automation"** → `ha-nova:review` (NOT read + review)
**"Review my utility meter helper"** → `ha-nova:review` (minimal config-entry helper review)
**"Show my automations"** → `ha-nova:read` (NOT review)
**"Show all automations with prefix kitchen_"** → `ha-nova:entity-discovery` (bulk inventory, not full YAML dump)
**"Create an automation"** → `ha-nova:write` (NOT read + write)
**"Create an input_boolean"** → `ha-nova:helper` (NOT write)
**"Show my helpers"** → `ha-nova:helper` (NOT read)
**"Review all automations in area Living Room"** → `ha-nova:review` (area-first aggregate review when more than one target resolves)
**"Create a timer"** → ambiguous! Ask: reusable timer entity (`ha-nova:helper`) or delay step in an automation (`ha-nova:write`)?
**"Show my energy dashboard"** → `ha-nova:fallback` (no dedicated skill)
**"Import a blueprint"** → `ha-nova:fallback` (relay-ready, no skill)
**"How do I manage Apps?"** → `ha-nova:fallback` (external, web search)
**"Show history for sensor X"** → `ha-nova:fallback` (relay-ready, no skill)
**"Modify my dashboard"** → `ha-nova:fallback` (NEVER raw `lovelace/config/save` without this skill)
**"Save the Lovelace config"** → `ha-nova:fallback` (NEVER direct WS write without read-merge-verify)

After any `read` or `review` task, re-evaluate intent once before continuing:
- config change on automation/script → `ha-nova:write`
- helper change → `ha-nova:helper`
- pass along the resolved identifiers needed by the next skill:
  - automation/script: `entity_id`, `unique_id`, current config
  - helper:
    - storage-based family: `entity_id`, helper type, internal helper id when already known
    - config-entry family: `entry_id`, domain, title, linked entities when already known
- always pass along the requested change
- keep this sequential: one skill at a time, never parallel
- for multi-target scope, keep the same safety and evidence rules; see `skills/ha-nova/bulk-patterns.md`

**Problem-description intents** ("X doesn't work", "Y is wrong", "stopped working"): dispatch to `ha-nova:review`. Review will analyze the config AND check current entity state — if an acute fix is possible, it offers a Quick-Fix service call at the end. Bulk review is the exception: it stays read-only and does not offer Quick-Fix.

## Latency Policy

- Prefer one-shot reads over multi-step probing.
- For first read/list, try Relay `/ws` directly.
- For write flows, keep main-thread file reads minimal:
  - context skill (this file)
  - `skills/ha-nova/bulk-patterns.md` only for multi-target discovery/review work
  - `skills/ha-nova/relay-api.md`
  - one agent template per phase
- No proactive doctor in success path.
- Re-read full state snapshot only with explicit reason.
