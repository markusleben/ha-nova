---
name: dashboard
description: Use when listing, reading, building, redesigning, updating, or deleting Home Assistant dashboards, views, and cards (installed custom cards included), checking dashboards for broken cards, or managing Lovelace resources through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Dashboard


## Scope

- list dashboards
- inspect the current dashboard structure: views, cards, badges, custom card types
- broken-card sweep over every dashboard (read-only)
- build a whole dashboard or view; add, reorder, and delete views; add, update, move, and delete cards; configure installed custom cards; set a view theme
- create a new storage dashboard shell, with content or a strategy config
- update dashboard metadata
- list Lovelace resources; create, update, and delete Lovelace resources
- delete an existing storage dashboard, and restore it from its config snapshot

Writes go only to `mode: storage` dashboards. A `yaml` dashboard is read-only here: say that its config lives in a YAML file (`ha-nova:yaml-config`) and offer a storage copy (a new dashboard with that content).

Not in scope: energy dashboard preferences (`ha-nova:energy`), installing a custom card (`ha-nova:hacs`), screenshots through the Relay (a client's own browser may look).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Dashboard content goes through typed verbs only:
- Read: `ha-nova get --dashboards` (every dashboard: `url_path`, `title`, `mode`, counts, `issues` — the sweep); `ha-nova get --dashboard <url_path>` (`layout` card list, `custom_types` → the resource that provides each, `resources`, `issues`); add `--config` for the full config (`.result.dashboards[0].config`).
- Plan (read-only): `ha-nova plan write --domain dashboard --id <url_path> --base <config_digest> --file <config.json>` (`--base` = the `config_digest` of the read the draft starts from); a new dashboard takes `--create --title "<title>" [--icon mdi:<name>] [--require-admin] [--hide-sidebar]` instead of `--base` (the `url_path` needs a hyphen); `ha-nova plan delete --domain dashboard --id <url_path>`; `ha-nova plan revert [--target <url_path> --domain dashboard]`.
- Execute: `ha-nova apply --plan <id> [--confirm <code>] [--no-snapshot]`.

Raw file-based WS (`ha-nova relay ws --data-file <payload-file>`) only for metadata, resources, and themes:
- `lovelace/dashboards/update` with `dashboard_id` (`lovelace/dashboards/list` is the source of truth: `dashboard_id` for `lovelace/dashboards/update|delete`, `url_path` for `lovelace/config|save`)
- `lovelace/resources`, `lovelace/resources/create`, `lovelace/resources/update`, `lovelace/resources/delete`
- `frontend/get_themes` (read)

Never send `lovelace/config/save`, `lovelace/dashboards/create`, or `lovelace/dashboards/delete` by hand: the plan does. Shape: `{"type":"lovelace/config/save","url_path":"<url_path>","config":{"views":[...]}}` — the whole document goes under `config`, and whatever the draft leaves out is gone. The default dashboard is `--id lovelace`; the CLI omits `url_path` when it is not listed.

## Flow

Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

1. Resolve: `ha-nova get --dashboards`; match `url_path` or title; ask one blocking question only if more than one still matches. `mode: empty` = no stored config yet (a new shell, or the auto-generated default).
2. Inspect: `ha-nova get --dashboard <url_path> --config`; save `config` to a client-private scratch file as the draft basis and keep its `config_digest` for `--base`. jq filters null-guard absent keys: `(.views // [])[]`, never bare `.views[]`; same for `.cards`, `.sections`, `.badges`.
3. Draft the WHOLE config (base = current, overlay = the change; unrelated views and cards stay byte-for-byte):
   - Whole dashboard or view: a `sections` view per floor or area (`config/floor_registry/list`, `config/area_registry/list`), a `heading` card first in each section, built-in `tile` cards; mobile-first: most-used controls first, about 8 cards per section at most, no wide horizontal stacks.
   - Built-in cards: any type documented on home-assistant.io/dashboards, with options from that card's page only. Template-capable fields (Markdown `content`) run the Render Loop (`skills/ha-nova/template-guidelines.md`) before the preview.
   - Custom cards: only a `custom:` type whose resource is installed (`custom_types`/`resources` in `get --dashboard`). Never invent its options: read its documentation first (`ha-nova:hacs` repository info or the card's README) or copy the shape of its existing usage in the user's dashboards. Not installed: say so and offer `ha-nova:hacs`; `card_mod` keys only when card-mod is installed.
   - Strategy: `{"strategy":{"type":"areas"}}` or `{"strategy":{"type":"original-states"}}` as the ENTIRE config — Home Assistant generates all views and cards at render time. Switching a populated dashboard TO a strategy discards its stored views: name that in the preview.
   - View `theme`: only a name from `frontend/get_themes`.
   - Resolve every entity with `ha-nova find`; never guess an id.
4. Plan: `ha-nova plan write …`. Exit 3 is normal (`plan.confirm` `natural`). Exit 4: `UNKNOWN_ENTITY` → resolve and fix the draft; `DASHBOARD_NOT_STORAGE` → read-only, explain; `CONFIG_ID_IN_USE` on a create → first check whether an earlier apply created it (then never create another), else another `url_path`. Exit 2 `PLAN_DRIFT` → the dashboard changed since that read (an edit in the UI): read again, re-merge, plan again. Exit 5 `NOT_FOUND` → its candidates.
5. Preview: the plan's `diff` rows verbatim in the Changes slot, plus a plain-language behavior line (write-safety → Behavior narrative) that names every removed view and card. Name each `result.issues` entry with `new: true`: a new `card_type_unconfirmed` card blocks the save until the user confirms it is installed; say a disabled entity plainly. Say: a dashboard editor left open in Home Assistant would write its old copy back — close it first. Close the card with the plan's `say`.
6. Confirm: writes take a natural "yes". Delete takes the plan's typed confirmation code `confirm:<token>` (`plan.code`), never a menu.
7. Apply: `ha-nova apply --plan <id>` (`--confirm <the user's exact text>` for a delete). Apply re-reads the live dashboard first, and again right before the save: exit 2 means it changed since the preview (an edit in the UI) — nothing was saved; read it again, re-merge, re-preview. Exit 6 `CONFIG_REJECTED`: status `failed` = nothing changed; `unverified` = it may have landed — relay its message and `next`. Exit 7: unknown; never retry. `SNAPSHOT_FAILED` saved nothing: offer pruning the snapshot store, a full safety backup (`ha-nova:backup`), or proceeding without one; only after the user picks the last, `apply --plan <id> --no-snapshot` (plus `--confirm` for a delete).
8. Result: `verified` = the read-back equals the plan (a create: also listed; a delete: no longer listed). Name the `snapshot` and `undo` receipt; offer `revert` for an update. A create is cleaned up through the delete flow. A deleted dashboard restores from its snapshot (`skills/ha-nova/config-snapshots.md`) by the delete result's `restore` (a `--create` with the shell's title and flags; the main `lovelace` dashboard: a plain write) — the `dashboard_id` is new.
9. Broken-card sweep: `ha-nova get --dashboards`; report per dashboard each `missing_entity` (with its `detail`) and `card_type_unconfirmed` (no installed resource seems to provide it — missing, or loaded another way, e.g. `extra_module_url`). Nothing found: "No broken cards found in <n> dashboards." Fixes: one dashboard at a time, steps 2–8.
10. Metadata update: preview the exact fields, confirm, then `lovelace/dashboards/update` with `dashboard_id`; only send changed metadata fields supported there: `title`, `icon`, `show_in_sidebar`, `require_admin`; do not resend `url_path`, `mode`, or unrelated config fields in the update payload. Verify through `lovelace/dashboards/list`.
11. Resources: create/update previews `res_type` and `url`, confirm, call, verify through `lovelace/resources`; delete previews the exact resource and requires `confirm:<token>`, called with `resource_id`.
12. Several changes in one dashboard are ONE draft and one plan; a card batch within one dashboard executes as ONE merged `lovelace/config/save` (sequential per-card saves would overwrite each other). Multi-item deletes follow `skills/ha-nova/batch-safety.md`; dashboards, resources, and cards are separate families — one family per manifest, each dashboard delete with its own plan code. An entity reference on an existing dashboard may join a non-destructive grouped change set ONLY as the downstream operation of a dependency-bound set (`skills/ha-nova/grouped-change-set.md` → Dependency-Bound Outputs); a dashboard-card removal may join a cross-family destructive cleanup only through its manifest (`grouped-change-set.md` → Cross-Family Destructive Cleanup).
13. If verification fails, stop and report the mismatch. Do not retry by guessing.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there (`skills/ha-nova/output-cards.md`).

For list/read/sweep:
- `Dashboard`
- `Target`
- `Summary`
- relevant inventory or issue excerpt only

For create/update/delete:
- `Dashboard`
- `Mode`
- `Planned change`
- `Save status` / `Delete status` before confirmation
- `Options` / confirmation-code prompt
- `Verification`
- `Next step`

Use stable localized slot labels in this order; omit empty slots, do not invent ad-hoc headings. List/read renders the Report shape; dashboard and resource inventories render the List Frame (output-cards.md).

Never dump the full dashboard JSON/YAML unasked (`show yaml` shows it).

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary` (an unambiguous everyday action, verified afterwards); everything else runs or is saved only after the user confirms the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes need a code.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No guessed `url_path` or `dashboard_id` values.
- Dashboard and resource delete uses exact confirmation code only, even for items created earlier in the same session. Removing views or cards inside a save is previewed (natural confirmation) because every save is recoverable: `ha-nova apply` takes an auto config snapshot first, and `revert` undoes the last verified save. Dashboard deletes restore partially from their snapshot; resources recover via Home Assistant Backups.
- A broad re-layout instead of a targeted edit: say so before writing.

## Guardrails

- Never use `lovelace/info` to decide whether a dashboard is writable.
- Never use `lovelace/config/delete` as the dashboard delete path.
- Never probe a different dashboard's config to infer the target's mode or structure; reading other dashboards for a custom card's existing usage is fine.
- If the target stays ambiguous after one clarification, stop and explain.
