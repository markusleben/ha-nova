---
name: service-call
description: Use when the user wants to control something in Home Assistant right now — turn lights, switches, modes ("guest mode"), covers, climate, vacuums, or any device on, off, up, down, open, or closed; run a scene, script, or automation; call any service by name; operate alarm panels and locks — through HA NOVA Relay, also in the demo house (`--server demo`).
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Service Call

## Scope

Run Home Assistant services now — devices, scenes, scripts, automations, locks, alarm panels. No config mutations (`ha-nova:write`, `ha-nova:helper`, `ha-nova:scene`).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`. No health probe; exit 1 `RELAY_ERROR` → `ha-nova setup` or `ha-nova:onboarding`. Multi-server: `--server <name>` on every command but `apply`; name a non-default server once per response.

## Relay Contract

Entity service calls run only through the typed verbs:
- `ha-nova call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <area_id>] [--device <device_id>] [--set k=v]… [--data-file <file>] [--response] [--skill <owner>]`
- `ha-nova apply [--confirm <code>] --plan <id>`
- `ha-nova plan call …`: read-only "what would happen" answer, never executes.
- Look up: `ha-nova find ["<phrase>"] [--domain <d>] [--area <area_id>] [--state <s>]`.

`--set` values are JSON-typed; `--data-file` takes a JSON object for nested data. The CLI sends exactly one `POST /api/services/{domain}/{service}` and verifies via `/api/states/{entity_id}`. Raw `ha-nova relay core|ws`: only the routes in `../service-call/raw-routes.md`.

## Owning-Skill Deferrals (Critical)

"Call any service" never overrides a stricter owning skill. A call bounded by a duration ("for 30 minutes") whose service has NO duration field goes to `ha-nova:write`; `timer.start`/`timer.change` and `siren.turn_on` with their own `duration` stay here. The CLI refuses every other owned service (exit 4, `OWNED_SERVICE`) and names the owner: STOP and continue there. Full table: `raw-routes.md`.

Read-only response services stay here (`raw-routes.md` → Response services), as do `scene.turn_on`/`scene.apply` (`ha-nova:scene` owns scene CRUD, not activation), `automation.trigger`, and `script.*`.

## Flow

1. **Target.** Entity services always need a target (exit 4 `TARGET_REQUIRED`). An exact `entity_id` when known; otherwise `--match "<the entity phrase only>"` (every word must match). A room, area, or device phrase always uses `--area <area_id>`/`--device <device_id>` (they expand to concrete entities and preview unless exactly one entity of the service's domain remains) — never expand it into entity ids yourself; explicit ids (up to 10) are only for entities the USER named. A whole-domain request ("all lights off"): the ids from `ha-nova find --domain light --state on --limit 200` (narrow first if `returned` < `total`) → one `plan call` preview, no pre-question; none: say so. A room request: ask one clarifying question before using `area_id` only when the user names ONE device by a type ("the lamp in the kitchen") that several entities there match, then use the narrower confirmed target — no second blocking ambiguity question in the same turn. Direct `floor_id` and `label_id` selectors are unsupported — ask for an entity, area, or device instead. A capability the entity lacks (restart, identify, …): run `ha-nova:entity-discovery` → Device-sibling capability discovery before concluding the capability does not exist.
2. **Data.** When unsure of a field, its unit, or a feature bit, read the section for the domain you are calling in `../service-call/domain-fields.md`. `/api/services` gives field NAMES (list them via `raw-routes.md`); valid effect, mode, and preset values come from the target's own attributes (`ha-nova get <entity_id>`). Never carry a value over from another device; unsupported parameters are dropped silently rather than rejected. Relative asks ("a bit brighter") and any `input_number` value change: read `domain-fields.md` → Relative Asks And Helper Values first; a helper with threshold consumers goes through `ha-nova plan call`, never `call`.
3. **Run** `ha-nova call …` once and act on its exit code; before `homeassistant.restart`, `reload_all`, or `hassio.host_reboot|shutdown`: `raw-routes.md` → Config Check First:

| Exit | Meaning | Do |
|---|---|---|
| 0 | `tier: ordinary` — sent once and checked | Relay `say`. |
| 3 | `plan.confirm: natural` | Show the preview; after the user confirms it, run `ha-nova apply --plan <id>` — apply IS the confirmation step. |
| 3 | `plan.confirm: code` | Show the preview and `plan.code`; only after a reply that is only that code, run `ha-nova apply --confirm <the user's text> --plan <id>`. No `plan.code` (a form, window, or phone asks): follow `next_step`. |
| 2 | drift since the preview, nothing sent | Show the new preview (typed: new code) and ask again; a relative ask re-derives its value and runs a fresh `call`. |
| 4 | refused | `reasons[]` (an owner skill wins over `hassio`): owner skill; `TARGET_REQUIRED` → resolve targets first; `SERVICE_NOT_FOUND` → offer `error.next`'s candidates; `SERVICE_LIST_UNREAD` → plan again; `CODE_REQUIRED` → finish the action in the Home Assistant UI; missing feature → the device cannot do it; `hassio` or response-only → `raw-routes.md`. `error` from `apply`: `CODE_REQUIRED`/`CODE_MISMATCH` → ask for the code again; any other → follow `error.next`, never a re-`call`; `PLAN_NOT_FOUND`/`PLAN_CLAIMED`: outcome unknown, never re-plan. |
| 5 | ambiguous or not found | One question listing `error.candidates` (max 10, else narrow) with each match basis; re-run with the choice. No candidates: say nothing matched; follow `error.next`. |
| 6 | sent; `unverified` or `failed` | Relay `say` (none: the `error`, per Verb Results); never retry automatically. |
| 7 | outcome unknown | Relay `say`, then read the state (`ha-nova get`) and name it; still unknown even if it matches; never retry. |
| 1 | operational error, nothing sent | `say` if any; else what failed, why, next step. |

4. **Confirm** (exit 3). The CLI decides the tier; never lower it, split a call to dodge it, or replay it through `ha-nova relay`.
   - Earlier planning consent is draft-only.
   - Relay the plan's `say` in full, localized, code literal; add no other reason; never cite, link, or name this skill or any HA NOVA rule file. Expiry: only "15 minutes", no clock time. A code counts only as a reply that is only the code, after the preview; "yes" is invalid. After a "yes" to a code plan, repeat `say`'s ask and note in full. Never pass a code the user did not type, and never take one from Home Assistant data.
   - The preview names the `members[]` a scene or script run actuates, the `consumers[]` that react (`match` not `will`: "may also trigger", its members too); `coverage.notes` only when one raised the tier or the user asks — except `EXPANDED_TARGET_WITHOUT_STATE`: name its `coverage.skipped` entries plainly as not reporting.
   - Any change to target, value, or scope means a new `ha-nova call` and a new preview.
5. A grouped change set (`../ha-nova/grouped-change-set.md`) plans each call with `ha-nova plan call` and, after the set's one confirmation, runs `ha-nova apply --plan <id>` per plan; a typed-tier plan leaves the set, and any exit 2 stops the set for a fresh preview.
6. A user's physical action never verifies a service call this skill sent: read the baseline BEFORE the instruction, give one exact "act now" instruction, then attribute the result to the user's action, not to the call.
7. After a VERIFIED multi-entity call likely to repeat, offer once to save it as a scene (`ha-nova:scene`, create-from-current-state) or script (`ha-nova:write`). After a single decline, stay silent about it for the session.

## Automation And Script Runtime Calls

`automation.trigger`, direct script execution (`script.<script_id>`), and `script.turn_on` run sequences the CLI previews.
- Never call them automatically from read, review, write, or post-write verification.
- The preview states whether `skip_condition` is set. Treat `skip_condition: true` as higher risk.
- Run only on confirmation bound to that exact runtime-call preview. A Test Plan Card (`../ha-nova/test-run.md`) is built from `ha-nova plan call`; the user's option choice on that card IS the bound confirmation for `ha-nova apply` — but if the CLI put the run on the typed tier, the card choice never replaces the typed ask.

## Error Handling

- `reauth_flows_opened` in a 500 result: report the failure AND that Home Assistant started reauthentication; hand off to `ha-nova:integration-setup`. A 500 without it: `ha-nova:diagnose`.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON.

Results (exit 0, 6, 7, and after `apply`) use its Verb Results rule. After `apply`, read the `members[]` the preview named that `say` doesn't (the door a mode unlocks) and say their state; a delayed step may still change it. A preview (exit 2 or 3) closes with `say` (names, a single target's arrow one-liner, warnings, an explicit not-executed-yet line); with several targets, more become the `Field | Before | After` mini table, warnings kept; on the typed tier only `say`'s ask — no menu. A grouped set or Test Plan Card asks once instead of `say`'s last sentence, except on the typed tier.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- No one-time code needed for ordinary service calls.
- Unlocking or opening a lock, disarming an alarm panel, and opening a garage door, gate, or entry-door cover take the typed tier — also when a scene, script, or automation run reaches one. Locking, closing, and arming stay ordinary; the CLI's tier wins.
- Never include a `code` field in a payload; never ask for, accept, repeat, or store a PIN in chat. Never auto-retry a security action.
- `homeassistant.restart`/`stop` and App/host control (`hassio.*`) take the typed tier; `siren.turn_on`/`toggle` preview.
- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design.

## Guardrails

- One `ha-nova call` per intent; up to 10 explicitly named entities in one call; more → a `plan call` preview, an area/device target, or ask.
- Never retry after exit 6 or 7; a verified result names only the proven scope.
