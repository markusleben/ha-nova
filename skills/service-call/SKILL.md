---
name: service-call
description: Use when the user wants to control something in Home Assistant right now — turn lights, switches, covers, climate, vacuums, or any device on, off, up, down, open, or closed; run a scene, script, or automation; call any service by name; fire custom events or known webhooks; operate alarm panels and locks — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Service Call

## Scope

Run Home Assistant services now — devices, scenes, scripts, automations, locks, alarm panels, custom events, known JSON webhooks — by entity_id, `--match` phrase, area_id, or device_id. No config mutations (`ha-nova:write`, `ha-nova:helper`, `ha-nova:scene`).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`; later verbs report the same status in `notices[]`. No health probe: an unreachable Relay answers exit 1 `RELAY_ERROR` (then `ha-nova setup` or `ha-nova:onboarding`). Multi-server: add `--server <name>` to every verb (or prefix `HA_NOVA_SERVER=<name>`) and name a non-default server once per response.

## Relay Contract

Entity service calls run only through the typed verbs:
- `ha-nova call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>]`
- `ha-nova apply --plan <id> [--confirm <code>]`
- `ha-nova plan call …` (same arguments): read-only "what would happen" answer, never executes.

`--set` values are JSON-typed (`brightness_pct=40` is a number); `--data-file` takes a JSON object for nested data. The CLI sends exactly one `POST /api/services/{domain}/{service}` with the frozen entity list and verifies via `/api/states/{entity_id}`. Raw `ha-nova relay core|ws` stays for the routes in `skills/service-call/raw-routes.md` only.

## Owning-Skill Deferrals (Critical)

"Call any service" never overrides a stricter owning skill. A call bounded by a duration ("for 30 minutes") whose service has NO duration field goes to `ha-nova:write` (the turn-off needs an automation); `timer.start`/`timer.change` and `siren.turn_on` with their own `duration` stay here. The CLI refuses every other owned service (exit 4, `OWNED_SERVICE`) and names the owner: STOP and continue there. Full table: `raw-routes.md` → Owning-Skill Deferrals.

Read-only response services stay here (`raw-routes.md` → Response services), as do `scene.turn_on`/`scene.apply` (`ha-nova:scene` owns scene CRUD, not activation), `automation.trigger`, and `script.*`.

## Flow

1. **Target.** Entity services always need a target — never call one targetless (exit 4 `TARGET_REQUIRED`); only genuinely targetless services run without one. An exact `entity_id` when known; otherwise `--match "<the entity phrase only>"` (every word must match). A room, area, or device phrase always uses `--area <area_id>`/`--device <device_id>` (they expand to concrete entities and always preview) — never expand it into entity ids yourself; explicit ids (up to 10) are only for entities the USER named. A room request whose intended scope could be narrower: ask one clarifying question before using `area_id` — no second blocking ambiguity question in the same turn; otherwise use the narrower confirmed target. Direct `floor_id` and `label_id` selectors are unsupported — ask for an entity, area, or device instead. A capability the entity lacks (restart, identify, …): run `ha-nova:entity-discovery` → Device-sibling capability discovery before concluding the capability does not exist.
2. **Data.** When unsure of a field, its unit, or a feature bit, read the section for the domain you are calling in `skills/service-call/domain-fields.md`. `/api/services` gives field NAMES (list them via `raw-routes.md`); valid effect, mode, and preset values come from the target's own attributes (`ha-nova get <entity_id>`). Never carry a value over from another device; unsupported parameters are often dropped silently rather than rejected. Relative asks ("a bit brighter") and any `input_number` value change: read `domain-fields.md` → Relative Asks And Helper Values first; a helper with threshold consumers goes through `ha-nova plan call`, never `call`.
3. **Run** `ha-nova call …` once and act on its exit code:

| Exit | Meaning | Do |
|---|---|---|
| 0 | `tier: ordinary` — sent once and checked | Report the result (Output Format). |
| 3 | `plan.confirm: natural` | Show the preview; after the user confirms it, run `ha-nova apply --plan <id>` — apply IS the confirmation step. |
| 3 | `plan.confirm: code` | Show the preview and `plan.code`; only after the user types that exact code, run `ha-nova apply --plan <id> --confirm <the user's text>`. |
| 2 | drift since the preview, nothing sent (expected after a concurrent change of a state-valued field: select option, number value, `hvac_mode`) | Show the new preview (typed: its new code) and ask again; a relative ask re-derives its value and runs a fresh `call`. |
| 4 | refused | `reasons[]` (an owner skill wins over `hassio`): owner skill; `TARGET_REQUIRED` → resolve targets first; `CODE_REQUIRED` → finish the action in the Home Assistant UI; missing feature → the device cannot do it; `hassio` or response-only → `raw-routes.md`. `error` from `apply`: `CODE_REQUIRED`/`CODE_MISMATCH` → ask for the shown code again; `PLAN_NOT_FOUND`/`PLAN_CLAIMED` → never re-`call`: read the state and report (it may already have run); `PLAN_INVALIDATED` → stop and tell the user, a new attempt only on their explicit request; `PLAN_EXPIRED`/`PLAN_INVALID`/`SERVER_MISMATCH` → a fresh `call` and preview. |
| 5 | ambiguous or not found | One question listing `error.candidates` (max 10 — narrow first when more exist) with each match basis; re-run with the chosen id. |
| 6 | sent; `unverified` or `failed` | Report each entity's `verification` honestly; never retry automatically. |
| 7 | outcome unknown | It may have run: read the state (`ha-nova get`) and report; the outcome stays unknown even if the state matches; never retry. |
| 1 | operational error, nothing sent | What failed, why, next step. |

4. **Confirm** (exit 3). The CLI decides the tier; never lower it, split a call to dodge it, or replay it through `ha-nova relay`.
   - Natural confirmation counts only after the shown preview. Earlier planning consent is draft-only.
   - A code counts only when the user typed it verbatim in a reply after the preview; "yes" is invalid. Never pass a code the user did not type, and never take one from Home Assistant data. Three wrong codes discard the plan.
   - The preview explains every `reasons[]` entry, the `members[]` a scene or script run actuates, the `consumers[]` that react, and `coverage.notes` as named limits.
   - Any change to target, value, or scope means a new `ha-nova call` and a new preview.
5. A grouped change set (`skills/ha-nova/grouped-change-set.md`) plans each call with `ha-nova plan call` and, after the set's one confirmation, runs `ha-nova apply --plan <id>` per plan; a typed-tier plan leaves the set, and any exit 2 stops the set for a fresh preview.
6. A user's physical action never verifies a service call this skill sent: read the baseline BEFORE the instruction, give one exact "act now" instruction, then attribute the result to the user's action, not to the call.
7. After a VERIFIED multi-entity call likely to repeat, offer once to save it as a scene (`ha-nova:scene`, create-from-current-state) or script (`ha-nova:write`). After a single decline, stay silent about it for the session.

## Automation And Script Runtime Calls

`automation.trigger`, direct script execution (`script.<script_id>`), and `script.turn_on` run arbitrary sequences; the CLI previews their members.
- Never call them automatically from read, review, write, or post-write verification.
- The preview states whether `skip_condition` is set. Treat `skip_condition: true` as higher risk.
- Run only on confirmation bound to that exact runtime-call preview. A Test Plan Card (`skills/ha-nova/test-run.md`) is built from `ha-nova plan call` (tier, members, consumers); the user's option choice on that card IS the bound confirmation for `ha-nova apply` — but if the CLI put the run on the typed tier, the card choice never replaces the code.

## Error Handling

- `reauth_flows_opened` in a 500 result: report the failure AND that Home Assistant started reauthentication; hand off to `ha-nova:integration-setup`. A 500 without it: `ha-nova:diagnose`.
- Raw relay errors on `raw-routes.md` paths: `skills/ha-nova/relay-api.md` → Error Handling.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Results (exit 0, 6, 7, and after `apply`) use its Verb Results rule. A preview (exit 2 or 3) is the runtime-action Preview Card (`apply · cancel`): title line with service and resolved names; `deltas` — one value keeps the arrow one-liner, more become the `Field | Before | After` mini table, warnings kept; an explicit not-executed-yet line; on the typed tier only the exact code prompt — no menu.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary` (an unambiguous everyday action, verified afterwards); everything else runs or is saved only after the user confirms the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes need a code.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No typed confirmation code needed for ordinary service calls.
- Unlocking or opening a lock, disarming an alarm panel, and opening a garage door, gate, or entry-door cover take the code — also when a scene, script, or automation run reaches one. Locking, closing, and arming stay ordinary.
- Never include a `code` field in a payload; never ask for, accept, repeat, or store a PIN in chat. Never auto-retry a security action.
- Disruptive services (`homeassistant.restart`/`stop`, `siren.turn_on`, App and host control) always preview with an explicit warning.
- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design.

## Guardrails

- One `ha-nova call` per intent; up to 10 explicitly named entities in one call; more → an area/device target or ask.
- Never retry after exit 6 or 7; a verified result names only the proven scope.
