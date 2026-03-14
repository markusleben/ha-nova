# `ha-nova:guide` Skill Implementation Plan

> Historical plan note: command examples in this document may reflect the older relay-path era. Current public contract uses `ha-nova relay ...`, `ha-nova setup`, and `ha-nova update`.

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a fallback skill that guides agents when users ask about HA features not covered by existing skills — with concrete search queries, experimental relay call examples, and roadmap awareness.

**Architecture:** New `skills/guide/SKILL.md` (inline, no relay calls, pure guidance). Context skill gets a catch-all dispatch row. Three feature tiers: Relay-Ready / Roadmap / External.

**Tech Stack:** Markdown only. No code, no tests. Verification via dev-sync + new session.

**Design doc:** `docs/plans/2026-03-07-guide-skill-design.md`

---

### Task 1: Create `skills/guide/SKILL.md`

**Files:**
- Create: `skills/guide/SKILL.md`

**Reference files to cross-check payloads against:**
- `skills/ha-nova/relay-api.md` (relay CLI syntax)
- `docs/reference/ha-api-matrix.md` (WS types + REST paths)
- `docs/reference/bridge-architecture.md` (roadmap phases)

**Step 1: Create the file**

Write `skills/guide/SKILL.md` with the following exact content structure. The file must follow the existing skill conventions: YAML frontmatter, `# HA NOVA Guide` title, sections for Scope, Capability Map, Agent Flow, Feature Sections, Safety.

Content requirements per section:

**Frontmatter:**
```yaml
---
name: guide
description: Use when the user asks about Home Assistant features not covered by other ha-nova skills (dashboards, blueprints, history, energy, areas, zones, add-ons, HACS, etc.).
---
```

**Scope:** Fallback guidance skill. No relay calls of its own. Routes agent to: (1) experimental relay calls with safety warnings, (2) web search with concrete queries, (3) roadmap awareness.

**Capability Map:** Compact table showing ALL ha-nova features (including those covered by other skills) with status column. This gives the agent a single reference to check what's available. Format:

```markdown
| Feature | Status | Skill |
|---------|--------|-------|
| Automations CRUD | Covered | `ha-nova:read` / `ha-nova:write` |
| Scripts CRUD | Covered | `ha-nova:read` / `ha-nova:write` |
| Helpers (9 types) | Covered | `ha-nova:helper` |
| Entity Search | Covered | `ha-nova:entity-discovery` |
| Service Calls | Covered | `ha-nova:service-call` |
| Config Review | Covered | `ha-nova:review` |
| Relay Setup | Covered | `ha-nova:onboarding` |
| Dashboard/Lovelace | Relay-Ready | this skill |
| Blueprints | Relay-Ready | this skill |
| History Queries | Relay-Ready | this skill |
| Logbook Queries | Relay-Ready | this skill |
| Area/Floor CRUD | Relay-Ready | this skill |
| Label/Category CRUD | Relay-Ready | this skill |
| Zone/Person/Tag Mgmt | Relay-Ready | this skill |
| Energy Config | Relay-Ready | this skill |
| System Health | Relay-Ready | this skill |
| Calendar Queries | Relay-Ready | this skill |
| Config-Entry Helpers | Relay-Ready | this skill |
| Entity Registry Edits | Relay-Ready | this skill |
| Event Subscriptions | Roadmap (Phase 1c) | -- |
| Template/YAML Sensors | Roadmap (Phase 3) | -- |
| Backups | Roadmap (Phase 2) | -- |
| Add-ons / Supervisor | External | -- |
| HACS | External | -- |
| Zigbee/Z-Wave Config | External | -- |
```

**Agent Flow:** Decision tree instructing the agent how to handle each tier. Key rules:
- Relay-Ready: show experimental relay examples (with warning), offer web search
- Roadmap: explain phase + ETA, offer web search for manual workaround, suggest HA UI
- External: explain out-of-scope, offer web search, point to HA UI

**Feature Sections (18 total):** Each section follows this template:
```markdown
### [Feature Name] -- [STATUS TAG]

[1-2 sentence description of what this does in HA]

**Search:** `[concrete search query for WebSearch tool]`

[For Relay-Ready only:]
**Experimental relay calls (no skill guardrails):**
```bash
# Read
~/.config/ha-nova/relay ws -d '{"type":"..."}'
# Write (if applicable)
~/.config/ha-nova/relay ws -d '{"type":"...","field":"value"}'
```
**Risks:** [specific risks for write operations]

[For Roadmap only:]
**Status:** Coming in Phase X. Blocked by: [blocker].
**Workaround:** [what user can do now]

[For External only:]
**Why external:** [1 sentence reason]
**Alternative:** [how to do it via HA UI or other tools]
```

**12 Relay-Ready sections** (use exact WS types/REST paths from `ha-api-matrix.md`):

1. **Dashboard/Lovelace** -- WS `lovelace/config`, `lovelace/config/save`, `lovelace/info`. Risk: save overwrites entire dashboard.
2. **Blueprints** -- WS `blueprint/list`, `blueprint/import`. Search: `home assistant blueprint import automation yaml api 2026`.
3. **History Queries** -- REST `/api/history/period/{start}?filter_entity_id=...&end_time=...`. Read-only.
4. **Logbook Queries** -- REST `/api/logbook/{timestamp}`. Read-only.
5. **Area/Floor CRUD** -- WS `config/area_registry/{list|create|update|delete}`, `config/floor_registry/{list|create|update|delete}`. Fields: `name`, `icon`, `floor_id`, `labels`.
6. **Label/Category CRUD** -- WS `config/label_registry/{list|create|update|delete}`, `config/category_registry/{list|create|update|delete}`.
7. **Zone/Person/Tag Management** -- WS `zone/{list|create|update|delete}`, `person/{list|create|update|delete}`, `tag/{list|create|update|delete}`.
8. **Energy Configuration** -- WS `energy/get_prefs`, `energy/save_prefs`, `energy/validate`, `energy/info`.
9. **System Health & Repairs** -- WS `repairs/list_issues`, `system_health/info`. Read-only.
10. **Calendar Queries** -- REST `/api/calendars`, `/api/calendars/{entity_id}`. Read-only.
11. **Config-Entry Helpers** -- WS `config_entries/flow` (multi-step). Types: template, group, utility_meter, derivative, min_max, threshold, integration, statistics, trend, random, filter, tod, generic_thermostat, switch_as_x, generic_hygrostat. Risk: complex multi-step flows, easy to get wrong.
12. **Entity Registry Mutations** -- WS `config/entity_registry/update` (rename, disable, hide, change area/labels), `config/entity_registry/remove`. Risk: remove is irreversible.

**3 Roadmap sections:**

13. **Event Subscriptions** -- Phase 1c. Blocker: no SSE streaming endpoint. Workaround: poll `GET /api/states/{id}` periodically.
14. **Template/REST/CLI Sensors** -- Phase 3. Blocker: no filesystem access. Workaround: create via HA UI > Settings > Devices & Services > Helpers.
15. **Configuration Backups** -- Phase 2. Blocker: no backup endpoint. Workaround: use HA UI > Settings > System > Backups.

**3 External sections:**

16. **Add-ons / Supervisor** -- Supervisor API (separate from Core). Alternative: HA UI > Settings > Add-ons.
17. **HACS** -- Third-party community store, no stable API. Alternative: HACS web UI in HA sidebar.
18. **Zigbee/Z-Wave / Network** -- Device-level pairing requires specialized coordinators. Alternative: HA UI > Settings > Devices & Services > [Integration].

**Safety Guardrails section:** (at bottom of file)
- Always preview full payload before executing experimental relay calls
- Read before write: fetch current state first for destructive operations
- Explicit warning on every experimental call: "No skill guardrails. Proceed with caution."
- No batch writes: one resource at a time
- Delete requires tokenized confirmation (`confirm:<token>`)
- Never guess IDs: resolve via list/search first

**Step 2: Self-review the file**

Verify against reference docs:
- All WS types match `docs/reference/ha-api-matrix.md`
- All REST paths match `docs/reference/ha-api-matrix.md`
- All relay CLI syntax matches `skills/ha-nova/relay-api.md`
- All phase numbers match `docs/reference/bridge-architecture.md`
- File is under 400 lines
- No emojis (per project convention)

**Step 3: Commit**

```bash
git add skills/guide/SKILL.md
git commit -m "feat: add guide skill for uncovered HA features"
```

---

### Task 2: Update Context Skill Dispatch Table

**Files:**
- Modify: `skills/ha-nova/SKILL.md` (lines ~67-91, Skill Dispatch section)

**Step 1: Add catch-all row to dispatch table**

Add at the bottom of the dispatch table (after the `fix relay/auth/connectivity errors` row):

```markdown
| anything not matched above (dashboards, blueprints, history, energy, areas, zones, etc.) | `ha-nova:guide` |
```

**Step 2: Add dispatch examples**

After the existing examples block (after line ~90), add:

```markdown
**"Zeige mir mein Energy Dashboard"** -> `ha-nova:guide` (no dedicated skill)
**"Importiere einen Blueprint"** -> `ha-nova:guide` (relay-ready, no skill)
**"Wie manage ich Add-ons?"** -> `ha-nova:guide` (external, web search)
**"Zeige mir die History von Sensor X"** -> `ha-nova:guide` (relay-ready, no skill)
```

**Step 3: Self-review**

- Dispatch table still has exactly ONE skill per intent (no overlap)
- Catch-all is last row (most specific matches first)
- Examples are consistent with existing format (arrow style, parenthetical explanation)

**Step 4: Commit**

```bash
git add skills/ha-nova/SKILL.md
git commit -m "feat: add guide skill catch-all to context dispatch"
```

---

### Task 3: Sync and Verify

**Step 1: Run dev-sync**

```bash
bash scripts/dev-sync.sh
```

Expected: syncs to Claude Code plugin cache + Gemini installer + relay CLI.

**Step 2: Verify file exists in plugin cache**

```bash
ls ~/.claude/plugins/cache/*/ha-nova/*/skills/guide/SKILL.md 2>/dev/null || echo "NOT FOUND"
```

Expected: file path printed (not "NOT FOUND").

**Step 3: Verify line count**

```bash
wc -l skills/guide/SKILL.md
```

Expected: under 400 lines.

**Step 4: Verify no broken relay CLI references**

```bash
grep -c '~/.config/ha-nova/relay' skills/guide/SKILL.md
```

Expected: reasonable count (>10, matching the number of relay examples).

**Step 5: Spot-check a few WS types against ha-api-matrix.md**

```bash
# These should all appear in ha-api-matrix.md
grep -c 'lovelace/config' docs/reference/ha-api-matrix.md
grep -c 'blueprint/list' docs/reference/ha-api-matrix.md
grep -c 'energy/get_prefs' docs/reference/ha-api-matrix.md
```

Expected: each returns >= 1.

---

### Task 4: Create PR

**Step 1: Create feature branch (if not already on one)**

```bash
git checkout -b feat/guide-skill
```

**Step 2: Push and create PR**

```bash
git push -u origin feat/guide-skill
gh pr create --title "feat: add guide skill for uncovered HA features" \
  --body "## What

Adds \`ha-nova:guide\` — a fallback skill that guides agents when users ask about HA features not covered by existing skills.

## Why

Currently, when a user asks about dashboards, blueprints, history, energy, etc., the agent has no guidance. It either hallucinates or says \"not supported\".

## How

- New skill at \`skills/guide/SKILL.md\` with 18 feature sections across 3 tiers:
  - **Relay-Ready** (12): API works, no skill — provides experimental relay call examples + web search queries
  - **Roadmap** (3): planned for future phases — explains timeline + workarounds
  - **External** (3): out of scope — points to HA UI + web search
- Context skill dispatch table updated with catch-all route
- Compact capability map for quick agent reference

## Testing

- \`bash scripts/dev-sync.sh\` — syncs to plugin cache
- New session: ask about dashboards, blueprints, history — should route to guide skill
- Relay payloads verified against \`ha-api-matrix.md\`"
```

**Step 3: Wait for CI + review bot**

```bash
gh pr checks <nr> --watch
gh api repos/<owner>/<repo>/pulls/<nr>/comments
```

**Step 4: Address any findings, then merge**

```bash
gh pr merge --squash --delete-branch
```
