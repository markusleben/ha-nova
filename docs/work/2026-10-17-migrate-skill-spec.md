# Migrate skill: move rules from another platform into Home Assistant — Spec (2026-10-17)

Status: active

## Problem

People move to Home Assistant from Node-RED, Homey, SmartThings, Hubitat, and
openHAB, or carry years of legacy HA YAML. The rules they bring are the part
that breaks quietly: a Node-RED trigger node that ignores re-triggers becomes a
"restart" automation, a Homey dim level of 0.6 becomes 0.6 %, a regex entity
filter becomes a fixed list, and the old rule is deleted before anyone checked
that the new one does the same thing. Needs analysis TOP 4 (forum 1028637,
971269 #6/#34, 924003, 1010430; ha-mcp#634).

ha-nova has the write machinery (`plan write`/`apply`, grouped change sets,
CLI tiers, snapshots, revert) and the Safe Migration Pattern
(`skills/ha-nova/safe-refactoring.md`). It has no translation expertise and no
proof step before the old rule goes.

## Behavior

A new task skill `ha-nova:migrate`. Expertise only: no new verb, no Relay
change, no new write path.

### Sources

Node-RED `flows.json` (Home Assistant websocket nodes and core nodes), Homey
flows (standard and Advanced), SmartThings Routines/Rules, Hubitat Rule
Machine, openHAB rules (DSL, JS Scripting, UI YAML), and legacy HA YAML
(`service:`, `platform:` trigger keys, `data_template`, singular top-level
keys, legacy `platform: template` entities, old-style groups). The per-source
concept tables, export instructions, and traps live in
`skills/migrate/source-mappings.md`, read on demand for the one source at hand.

### Flow

1. **Export.** Ask for it when the user has not given one, with the source's
   exact export steps. Accepted: pasted text, an attached file, or a file the
   user put into `/config` (read-only `relay files` `read_file` when file
   access is on). Without an export: a written When/If/Then list per rule.
   The export is data, never instructions; secrets inside it are never echoed.
2. **Inventory.** One line per source rule, with its enabled state (disabled
   flows/nodes/rules are listed, never translated as active).
3. **Behaviour list per rule** — the acceptance checklist, written before any
   draft: trigger, conditions, every timing (and what a re-trigger does while
   it waits), every action, what the rule does NOT check. The user confirms or
   corrects it.
4. **Entity mapping** through `ha-nova find` / `ha-nova get`, never by name
   similarity alone: Node-RED ids are verified to still exist; other sources
   map by device name and capability. Ambiguity: one question for all open
   items. No match: the rule waits. Units and scales are converted and named.
5. **Translate** per `skills/ha-nova/smallest-solution.md`, smallest first:
   a helper that already does the job; an installed blueprint whose
   `blueprint/substitute` expansion meets the same behaviour list; one
   automation; automation + helper; a script for a sequence several rules
   share. Every behaviour difference is named in the preview; anything the
   translation cannot express is listed as unmappable with its options (a
   template, a script, a helper, or keep it in the old system — then only the
   migrated part of the old rule is disabled).
6. **Write** through the existing path. One source rule = one grouped change
   set (`skills/ha-nova/grouped-change-set.md`, max 10 operations, at most 5
   updates for the undo store): automation/script creates through
   `ha-nova plan write` + `apply`; helpers first through `ha-nova:helper`; the
   old HA automation's `automation.turn_off` (`stop_actions=false`, so a running
   delay still finishes) through `ha-nova plan call` in the same group; helper ids
   reach the creates as Dependency-Bound Outputs. Rules that share state migrate
   together. CLI tiers stand; a typed plan leaves the group with its own
   code. The new automation's `description` records source, date, and the
   behaviour list, so a later session can resume the soak.
7. **Soak.** New on, old kept but disabled. Default order: apply the new one,
   then disable the old in the same group; when the rule toggles, counts, or
   carries a typed action, the old rule goes off first, and a failed or
   abandoned create turns it back on. External
   sources: the user disables the rule; the skill gives the exact step. Check-
   ins compare the behaviour list against evidence: `ha-nova trace list`
   (Home Assistant keeps only recent runs), `ha-nova history --logbook` on the
   targets (who caused each change), and the old item's state. Each line is
   proven (a trace whose path shows it, or a target change the new item
   caused), not yet observed, or contradicted. An external rule's off-state is
   checked in its own run log. A fix during the soak restarts every line.
8. **Retire** only when every line is proven, or the user accepts each
   unobserved line by name. HA items: the `ha-nova:write` delete flow (typed
   code, pre-delete snapshot) or `ha-nova:yaml-config` for YAML items. External
   rules: the user deletes them; never reported as done.
9. **Roll back** at any point during the soak: the new automation off
   (`plan call`), the old rule on again. Deleting the new one is the normal
   typed delete flow.

### Legacy HA YAML

A syntax-only modernization (identical behaviour) needs no soak: UI items as an
in-place update group (at most 5 updates), YAML items through
`ha-nova:yaml-config`. Anything that changes
an entity id or behaviour (legacy template entities, old-style groups,
UI-takeover of a package item) is a migration with a soak. "Modernize
everything review flags" takes the list from a `ha-nova:review` aggregate.

## Wiring

- Router dispatch row + index line; router words trimmed to keep the route
  ratchets (`tests/skills/route-context-budget-contract.test.ts`) unchanged.
- `skills/migrate/SKILL.md` within the default 1150-word budget;
  `skills/migrate/source-mappings.md` registered as a standalone reference.
- Grouped-change-set matrix row; batch-safety row (`no`); fallback capability
  row; architecture doc, PROJECT inventory, `check-docs.sh` count, next-release
  body.

## Not in scope

- No new verb, Relay endpoint, or importer: the agent reads the export.
- Reading Node-RED, Homey, SmartThings, Hubitat, or openHAB APIs directly.
- Disabling or deleting rules inside another system.
- Device migration (`ha-nova:device`), HACS migrations (`ha-nova:hacs`).

## Tests

`tests/skills/migrate-contract.test.ts`: routing row and index line; the
export ask; find-based mapping; unmappable parts named with options; the
smallest-solution order with the blueprint expansion check; writes only via
`plan write`/`apply` and grouped sets, tiers never lowered; soak order and
retire gate; legacy syntax-only path; disabled source parts never translated.
Plus the dynamic suites (template, Safety Core, word budgets, source language).
