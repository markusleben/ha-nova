# Design: Review Quick-Fix (Ansatz B)

**Date:** 2026-03-11
**Status:** Approved

## Problem

When a user reports a problem ("Heizung steht falsch", "Licht geht nicht aus"), the Review skill analyzes root cause and suggests config improvements — but doesn't offer to fix the *current* acute state. The user has to manually follow up with a service-call request.

## Solution

Extend the Review skill with a `QUICK_FIX:` output section. After analysis, if the current entity state shows an obviously fixable problem, offer a concrete service-call to fix it immediately.

## Design

### Review Skill Changes (`skills/review/SKILL.md`)

1. **State read in Target Resolution**: Review already resolves target entities. After resolving, read current state of the primary target entity via `/api/states/{entity_id}`. Store for later use.

2. **New output section `QUICK_FIX:`** after `SUMMARY:`:
   - If current entity state contradicts what the automation/config intends (e.g., entity is `on` but should be `off` based on current conditions, or mode is wrong):
     - Show: what's wrong (current state), what should be (expected state), concrete service call to fix it
     - Ask user for confirmation (natural confirmation, same tier as service-call)
   - If no acute state problem detected: `QUICK_FIX: none`
   - If state could not be read: `QUICK_FIX: skipped (state unavailable)`

3. **Execution on confirmation**: If user confirms, execute the service call inline (same pattern as `ha-nova:service-call` — POST to `/api/services/{domain}/{service}`, verify state after). This is the only mutation Review is allowed to perform, and only after explicit user confirmation.

4. **Read-only scope adjustment**: Change "Forbidden: any write call, any mutation" to "Read-only analysis. Exception: after user confirmation, a single Quick-Fix service call may be executed to correct an acute state problem detected during review."

### Context Skill Changes (`skills/ha-nova/SKILL.md`)

1. **Dispatch table**: No change needed — "analyze, review, audit, check, find problems" already routes to `ha-nova:review`. Problem descriptions ("X geht nicht", "Y ist falsch") naturally match this intent.

2. **Add one line to Dispatch section** (after the table): Clarify that Review may offer a Quick-Fix service call when it detects an acute state problem.

### What does NOT change

- No new skill
- No new dispatch rules
- Service-call skill unchanged
- Review check catalog (S/R/P/M/F/H) unchanged
- "ONE skill per intent" rule preserved — Review handles it end-to-end

## Scope Boundaries

- Quick-Fix is limited to **simple state corrections** (turn on/off, set mode, set value). No config mutations.
- Max 1 Quick-Fix per review (the primary target entity only).
- If the fix is ambiguous (multiple possible corrections), skip Quick-Fix and note it in SUGGESTIONS instead.
- Quick-Fix is an *offer*, never automatic.

## Examples

**Input:** "Meine Heizung-Automation schaltet nicht richtig"
**Review finds:** R-06 violation (mode: single + delay), config suggestions
**Quick-Fix detects:** `climate.wohnzimmer` is currently `heat` at 25°C, but automation intent (based on conditions) suggests it should be `off`
**Output:** `QUICK_FIX: climate.wohnzimmer steht auf heat/25°C. Soll ich auf off setzen?`

**Input:** "Analysiere meine Licht-Automation"
**Review finds:** Clean config, no issues
**Quick-Fix detects:** Light is off, no indication this is wrong
**Output:** `QUICK_FIX: none`
