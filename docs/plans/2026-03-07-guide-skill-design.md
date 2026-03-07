# Design: `ha-nova:guide` Skill

**Date:** 2026-03-07
**Status:** Approved
**Author:** Brainstorming session

## Context

HA NOVA covers automations, scripts, helpers, entity discovery, service calls, review, and onboarding through 7 dedicated sub-skills. However, many HA features are either accessible via Relay but lack a skill (dashboards, blueprints, history, area CRUD, energy) or are planned for future phases (subscriptions, filesystem, backups) or completely external (add-ons, HACS, Zigbee pairing).

Currently, when a user asks about uncovered functionality, the agent has no guidance — it either hallucinates or says "not supported". This skill closes that gap by providing structured fallback guidance with concrete web search queries and experimental relay call examples.

## Decision Summary

| Decision | Choice |
|----------|--------|
| Location | New skill `skills/guide/SKILL.md` |
| Granularity | Fine — one section per feature (~18 sections) |
| Guidance style | Active web search (concrete query strings) |
| Relay hints | Yes, with experimental warning + example payloads |
| Roadmap differentiation | Yes — three tiers (Relay-Ready / Roadmap / External) |
| Capability map | Yes, compact table at top |
| Dispatch | Auto-fallback from context skill catch-all |
| Execution model | Inline (no relay calls, pure guidance) |

## Architecture

### New File

`skills/guide/SKILL.md` — monolithic, all content in one file.

### Modified File

`skills/ha-nova/SKILL.md` — add catch-all dispatch row + examples.

### SKILL.md Structure

```
1. Frontmatter (name, description)
2. Scope (what this skill does and doesn't do)
3. Capability Map (compact table: Feature | Status | Skill/Guide)
4. Agent Flow (decision tree for the agent)
5. Feature Sections (~18), each with:
   - Status tag
   - Description (1-2 sentences)
   - Search query (concrete, copy-paste ready)
   - Relay example where applicable (with EXPERIMENTAL warning)
   - Risk notes for write operations
6. Safety guardrails for experimental relay calls
```

## Feature Catalog

### Relay-Ready (API works via Relay, no dedicated skill)

| # | Feature | WS/REST | Read Example | Write Example |
|---|---------|---------|-------------|--------------|
| 1 | Dashboard/Lovelace | WS | `lovelace/config` | `lovelace/config/save` |
| 2 | Blueprint Import | WS | `blueprint/list` | `blueprint/import` |
| 3 | History Queries | REST | `/api/history/period/` | n/a |
| 4 | Logbook Queries | REST | `/api/logbook/` | n/a |
| 5 | Area/Floor CRUD | WS | `config/area_registry/list` | `config/area_registry/create` |
| 6 | Label/Category CRUD | WS | `config/label_registry/list` | `config/label_registry/create` |
| 7 | Zone/Person/Tag Mgmt | WS | `zone/list`, `person/list`, `tag/list` | `zone/create`, etc. |
| 8 | Energy Configuration | WS | `energy/get_prefs` | `energy/save_prefs` |
| 9 | System Health & Repairs | WS | `repairs/list_issues` | n/a |
| 10 | Calendar Queries | REST | `/api/calendars` | n/a |
| 11 | Config-Entry Helpers | WS | `config_entries/get` | `config_entries/flow` (multi-step) |
| 12 | Entity Registry Mutations | WS | `config/entity_registry/get` | `config/entity_registry/update` |

### Roadmap (Relay not ready yet)

| # | Feature | Phase | Blocker |
|---|---------|-------|---------|
| 13 | Event Subscriptions | 1c | No SSE streaming endpoint |
| 14 | Template/REST/CLI Sensors | 3 | No filesystem access |
| 15 | Configuration Backups | 2 | No backup endpoint |

### External (completely outside HA NOVA scope)

| # | Feature | Reason |
|---|---------|--------|
| 16 | Add-on / Supervisor Management | Supervisor API, not Core API |
| 17 | HACS (Community Store) | Third-party, no stable API |
| 18 | Zigbee/Z-Wave / Network Config | Device-level, requires specialized tools |

## Agent Flow

```
User asks about Feature X
  |
  v
Context skill: no other skill matches
  |
  v
Dispatch to ha-nova:guide
  |
  v
Agent reads Capability Map --> finds Feature status
  |
  +-- RELAY-READY:
  |     1. Show relay example (with EXPERIMENTAL warning)
  |     2. Preview before any write call
  |     3. Offer WebSearch with concrete query
  |
  +-- ROADMAP:
  |     1. Explain "coming in Phase X"
  |     2. Offer WebSearch for manual workaround
  |     3. Suggest HA UI as interim solution
  |
  +-- EXTERNAL:
        1. Explain why it's out of scope
        2. Offer WebSearch for how to do it in HA directly
        3. Point to HA UI / relevant HA docs section
```

## Safety Guardrails for Experimental Relay Calls

1. **Always preview** — show the full payload before execution
2. **Read before write** — for destructive calls (dashboard save, registry delete), read current state first
3. **Warn explicitly** — every experimental call must show: "This operation has no skill guardrails. Proceed with caution."
4. **No batch writes** — one entity/resource at a time
5. **No delete without tokenized confirmation** — same `confirm:<token>` pattern as write/helper skills

## Context Skill Changes

Add to dispatch table in `skills/ha-nova/SKILL.md`:

```markdown
| anything not matched above (dashboards, blueprints, history, energy, areas, zones, etc.) | `ha-nova:guide` |
```

Add examples:

```markdown
**"Zeige mir mein Energy Dashboard"** --> `ha-nova:guide` (no dedicated skill)
**"Importiere einen Blueprint"** --> `ha-nova:guide` (relay-ready, no skill)
**"Wie manage ich Add-ons?"** --> `ha-nova:guide` (external, web search)
```

## Estimated Size

- `skills/guide/SKILL.md`: ~300-350 lines (under 400 LOC limit)
- Context skill changes: ~10 lines added

## Verification

1. Create `skills/guide/SKILL.md` with all sections
2. Update context skill dispatch table
3. Run `bash scripts/dev-sync.sh` to sync to plugin cache
4. Start new Claude Code session — verify guide skill is discovered
5. Test dispatch: ask about dashboards, blueprints, history — should route to guide
6. Test relay-ready flow: verify example payloads are correct against relay-api.md
7. Test roadmap flow: verify phase references match bridge-architecture.md
8. Test external flow: verify web search guidance is actionable
