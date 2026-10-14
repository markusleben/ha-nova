# Typed Verbs + MCP Adapter

Status: active

## 0. Problem (measured, grounded)

- Today the service-call route has to read about 18.3k words before its first tool call: the router trio (`skills/ha-nova/SKILL.md` 5,652 + `session-bootstrap.md` 1,176 + `output-rules.md` 2,636), plus `service-call/SKILL.md` (5,201), plus `indirect-actuation.md` (3,629, "Read this before previewing any call", `indirect-actuation.md:11`). The write route measures 31,945 words (`tests/skills/route-context-budget-contract.test.ts:28`).
- In Claude Code, `hooks/session-start:21-23` injects the whole router plus the bootstrap (6,828 words) into every session, before any task runs.
- The bootstrap requires `ha-nova check-update --quiet` before the first relay command (`session-bootstrap.md:8-9`). That is one extra tool call per session.
- Safety is executed by hand. Examples: tiering (`service-call/SKILL.md:99`, `indirect-actuation.md:19-31`), freezing the target list (`service-call/SKILL.md:95`), drift checks (`write-safety.md:67-82`), read-back (`write/SKILL.md:128-150`). Confirmation codes are invented by the model; the digest is "a binding discriminator, not a security boundary" (`batch-safety.md:68-73`).
- Every HA call goes through `ha-nova relay core|ws` (`cli/relay.go:85-95`). Client permission rules therefore cannot tell reads from writes.
- Skills need a shell. The Relay explicitly has "No MCP protocol" (`docs/reference/bridge-architecture.md:387`). That stays true: MCP lives in the CLI.

## 1. Goals, non-goals, metrics

Goals
1. Move deterministic safety (target resolution, tiering, freezing, drift, confirmation binding, read-back) out of prose into Go verbs in `cli/`. The Relay stays a dumb pipe (`PROJECT.md:58`).
2. Split reads from writes at the argv level, so client permission rules can allow reads and ask on writes.
3. Fast path: an ordinary action on a resolved target runs in one CLI call and is verified.
4. Offer the same verbs over `ha-nova mcp` (stdio) for clients without a shell.

Non-goals (v1)
- Events and webhooks.
- `hassio.*` App control.
- Review checks (`skills/review/checks.md` stays prose).
- Reading options for template or group config entries.
- MCP over HTTP or SSE, MCP OAuth, MCP elicitation.
- Any Relay change.
- Any cross-invocation cache.

Success metrics (token = words × 1.35, an assumption; the oracle stays the word ratchet)

| Task | Today | Target |
|---|---|---|
| "turn off the kitchen light" | 35–52k tokens, 10–12 tool calls | ≤ 8k tokens; ≤ 3 tool calls (normal case: Skill + `ha-nova call`) |
| Mandatory words, service-call route | ≈ 18.3k | ≤ 3,000 (new ratchet row) |
| Automation create (single, unambiguous) | ≈ 43k+ tokens | ≤ 25k tokens; mandatory words for the write route ≤ 15,500 |
| "unlock the front door" | prose code | exactly one extra user turn; the CLI rejects any `apply` without the minted code |

Safety invariants (each one has a Go test)
- **I1** Read verbs (`find|get|history|trace|plan`) never send a mutating request. The fake relay asserts that `/core` sees only `GET` and that `/ws` types come from a fixed read allowlist.
- **I2** A typed-tier plan never runs without its exact code.
- **I3** At `apply`, the plan is recomputed. Any hash mismatch gives exit 2 and nothing is sent.
- **I4** An unread user-authored member, a failed scan, or a limit cut is never classified below typed (`indirect-actuation.md:352-366`).
- **I5** Expanded targets (area, device, group, stored members) never take the fast path.
- **I6** The payload sent equals the previewed frozen `entity_id` list (`service-call/SKILL.md:95`).
- **I7** Exactly one POST per execution. No automatic retry, ever. This is stricter than `relay-api.md:590`.
- **I8** Plans are single-use and expire.
- **I9** `ha-nova relay *` behaviour is unchanged, and its existing tests stay green.

## 2. Verb set

The dispatch in `cli/main.go:65-133` gains `find get history plan call apply mcp`. `trace`, `diff` and `snapshot` already exist. Every verb accepts `--server` and `--via` like `relay` (`cli/relay.go:136-137`), and `--json` is the default output.

| Class | Verb | Syntax |
|---|---|---|
| read | find | `ha-nova find "<phrase>" [--domain d] [--area <area_id>] [--limit 20]` |
| read | get | `ha-nova get <entity_id> [--config] [--related]` (`--config` for automation/script/scene: resolves `unique_id` through the registry, never the slug, `relay-api.md:131-158`) |
| read | history | `ha-nova history <entity_id> [--since 24h] [--logbook]` (window ≤ 7d, ≤ 200 rows, summarized) |
| read | trace | unchanged (`cli/trace.go`) |
| read* | plan | `ha-nova plan call <domain.service> <targets>`, `plan write --domain automation\|script [--id <cfg_id>] --file <draft.json>`, `plan delete <automation.x\|script.x>`, `plan revert [--target <id>]`. Only writes a local plan file. |
| write | call | `ha-nova call <domain.service> [<entity_id>…] [--match "<phrase>"] [--area <id>] [--device <id>] [--set k=v]… [--data-file f]` |
| write | apply | `ha-nova apply --plan <id> [--confirm <code>]` |

- `--match` resolves only when exactly one non-disabled entity in the service's domain contains every phrase token. Matching is case- and diacritic-folded over `entity_id`, name and aliases (`entity-discovery/SKILL.md:80-83`). Otherwise the verb exits 5 with candidates (max 10, each with its match basis).
- `--set` values are JSON-typed when they parse (`brightness_pct=40` becomes a number). This removes payload files for the common case.
- `floor_id` and `label_id` selectors are refused (`service-call/SKILL.md:95`).

Output envelope (`schema_version` 1; fields are omitted when empty):

```json
{"schema_version":1,"verb":"call","server":"default","status":"verified","tier":"ordinary",
 "plan":{"id":"p_3fa9c1d20b7e","hash":"sha256:…","expires_at":"…Z","confirm":"none|natural|code","code":"confirm:unlock-front_door-3fa9c1"},
 "action":{"service":"light.turn_off","entity_ids":["light.kitchen"],"data":{}},
 "resolved":[{"entity_id":"light.kitchen","name":"Kitchen","by":"exact|match|area|device|group"}],
 "deltas":[{"entity_id":"light.kitchen","field":"brightness","before":"100%","after":"0%"}],
 "members":[{"entity_id":"lock.front_door","action":"lock.unlock","via":"script.leave/sequence/2","class":"typed"}],
 "consumers":[{"automation":"automation.night","match":"trigger","class":"ordinary"}],
 "reasons":[{"code":"ACCESS_LOCK","entity_id":"lock.front_door"}],
 "coverage":{"complete":true,"notes":["TEMPLATES_NOT_CHECKABLE","NO_LOCAL_CONSUMER_MANAGER"]},
 "diff":["| Trigger | 07:00 | 06:30 |"],
 "verification":[{"entity_id":"light.kitchen","expect":"off","observed":"off","outcome":"verified"}],
 "notices":[{"kind":"update|relay_outdated","text":"…"}],
 "outcome_unknown":true,
 "error":{"code":"AMBIGUOUS_TARGET","message":"…","candidates":[],"next":"…"}}
```

- `deltas` are rendered by the CLI: brightness in %, units, `unavailable` and `unknown` warnings. This replaces `service-call/SKILL.md:100-118`.
- `diff` is the existing `renderConfigChanges` output (`cli/diff.go:22-80`), passed through verbatim.
- `outcome` uses the existing vocabulary `verified|accepted|unverified|failed` (`outcome-verification.md:48-65`).
- `notices` come from the cache-only update nudge (`cli/update_nudge.go:49-55`). They replace the separate `check-update` call.

Exit codes. 2 is reused from `snapshotExitDrift` (`cli/snapshot_store.go:81`).

| Code | Meaning |
|---|---|
| 0 | read ok, plan stored by `plan`, or executed with outcome `verified`/`accepted` |
| 1 | operational error, nothing sent (existing convention) |
| 2 | drift: recomputed plan ≠ stored plan; a new plan is returned; nothing sent |
| 3 | needs confirmation: plan stored, `tier` is `preview` or `typed` |
| 4 | refused: service owned by another skill, unsupported selector, code handoff to the UI, unsupported feature bit, plan missing/expired/server mismatch, code mismatch |
| 5 | target ambiguous or not found |
| 6 | sent, outcome `unverified` or `failed` |
| 7 | outcome unknown after send (existing `OUTCOME_UNKNOWN` path, `cli/relay.go:837-904`); never retry |

### 2.1 Plans, hashes, confirmation codes

- **Store.** `paths.CacheDir/plans/<id>.json`, mode 0600, written through `writeFileAtomic`. TTL 15 min, at most 20 files (pruned on write). Each plan records its server profile.
- **Hash.** `sha256(canonical JSON{kind, server, action, tier, reasons, basis})`, where `action` always carries the full operation identity: for `call` the service, frozen `entity_ids` and data; for `write`/`delete`/`revert` the operation, config domain, target config id, and endpoint path. Two plans for different targets can never share a hash.
- **Basis for `call`.** Per acted entity: `pl`, `device_class`, `supported_features`. Per frozen target: the `before` value of every data field the call sets, and the `state` only when the service changes it. Plus the sha256 of every member and consumer config, normalized with `normalizeConfig` (`cli/diff.go:139`). Reasons enter the hash as code + entity + via only (no free-text detail); listener surpluses already show in tier and reasons, so raw listener counts are not hashed.
- **Basis for `write`/`delete`/`revert`.** The digest of the normalized current config (`"absent"` on create, which also asserts the id is unused) plus the draft digest. Revert reuses `selectUndoSnapshot` and `snapshotMatchesLive` (`cli/snapshot_store.go:197,286`).
- **Nonce.** Each plan draws a random 16-byte nonce. `id = "p_" + hex(sha256(hash ‖ nonce))[0:12]`, so a re-plan never overwrites a plan or replays its code; the hash itself stays nonce-free and is what `apply` recomputes.
- **Code.** `confirm:<verb>-<object_id≤24>[+N]-<id[2:8]>` (`+N` counts further frozen targets). Only typed-tier plans carry one. `apply` derives the expected code (and checks the plan id) from the RECOMPUTED plan and the stored nonce, never from file fields, and compares exactly after a whitespace trim. After 3 mismatches the plan is discarded; that counter lives in the local plan file and guards against slips, not against anyone who can edit the file.
- **`apply` order.** First **claim the plan atomically** (`os.Rename` of `<id>.json` to `<id>.claimed-<pid>`; a failed rename means another applicant holds it → exit 4, nothing sent). Then re-validate kind, schema version, and the stored request (static refusals, data keys), check expiry, server, and the frozen-target guard, then **recompute the full plan from the stored request**. If the hash changed: exit 2 with the new plan and code, and invalidate the old one. If equal: check id and tier against the recomputed plan, require the code iff the recomputed tier is typed, execute once, verify, delete the claimed file. A claim left by a crashed process is never reused (single-use, I8).
- **Honesty, stated in `docs/reference/safety.md`.** The CLI cannot tell whether a human typed the code. The code binds a confirmation to one exact plan, adds expiry and drift refusal, and moves the choice of tier out of prose. The human gates are (a) the skill rule that the code must come verbatim from a user message, and (b) client permission rules on `ha-nova apply`, which become possible now that argv separates reads from writes.

### 2.2 Deterministic tiering and consumer scan (`cli/tier*.go`)

Each step is the code form of an existing prose rule, cited per step.

1. **Resolve and expand targets.**
   - Registry list rows carry neither aliases nor `device_class` (verified on HA 2026.10.1). Aliases come from `config/entity_registry/get_entries` for the in-scope candidates (chunks of 500); every `device_class` comes from state attributes. `plan`/`call --match` always runs this alias pass before accepting a unique match; `find` only lists and reads aliases on a name miss.
   - Explicit ids and `--match` use `config/entity_registry/list_for_display` once (`ei, en, pl, di, ai`; `relay-api.md:179`). The full `config/entity_registry/list` is read only when the compact match finds nothing; aliases (`get_entries`) only when its names miss too.
   - An explicit id with no registry row (YAML entity without `unique_id`) resolves through an exact `GET /api/states/<entity_id>`. Its platform is unknown, so classification fails closed: typed when the domain or `device_class` is access-capable, preview otherwise — never the fast path.
   - `--area` uses `search/related` on `item_type:"area"` and takes `.data.entity`, filtered by the service domain (`service-call/SKILL.md:95`).
   - `--device` filters the compact registry by `di`.
   - Groups (`attributes.entity_id` is an array) expand to their leaves with `membership_source: attributes`.
   - Expansion always produces a frozen `entity_ids` list (I6).
2. **Classify the direct action** with a static table that mirrors `SKILL.md:99` and `service-call/SKILL.md:293-295`.
   - Cover classes `awning`, `blind`, `curtain`, `damper`, `shade`, `shutter`, `window` are non-access; every other class (garage, gate, door, custom, none) is access-capable.
   - On an access-capable entity (lock, alarm panel, access-capable cover) the table is an ALLOWLIST: only `lock.lock`, `cover.close_cover`, `alarm_arm_*`, `homeassistant.update_entity` (and `homeassistant.turn_off` where it maps to `close_cover`) stay below typed; `alarm_trigger` stays preview (disruptive). Every other service on such a target — `stop_cover`, tilt, integration services such as `zwave_js.set_value` — is typed.
   - A service of a non-core (integration) domain acts on the target's DEVICE: typed when any entity on that device is access-capable (`DEVICE_ACCESS`), otherwise at least preview — targetless or not. `esphome.*`, `pyscript.*`, `netdaemon.*` are typed.
   - A switch wrapped by an access-capable `switch_as_x` entity (same device, or both device-less) is typed (`SWITCH_AS_X_ACCESS`).
   - Typed: `lock.unlock|open`, `alarm_control_panel.alarm_disarm`.
   - Typed: open-capable cover services (`open_cover`, `toggle`, `set_cover_position` with position > 0, `indirect-actuation.md:327-337`) on `device_class` ∈ {garage, gate, door}. A cover with no `device_class` also goes typed (fail closed; see Q3).
   - Preview (disruptive): `homeassistant.restart|stop`, `siren.turn_on`, `alarm_trigger`.
   - Refused, with the owning skill named: the deferral table (`service-call/SKILL.md:43-59`), `hassio.*`, code-bearing locks and alarms (`service-call/SKILL.md:224-226`), unsupported feature bits (`service-call/SKILL.md:216-222`).
3. **Gate entry.**
   - The target domain is `scene|script|automation|group`.
   - The service writes a trigger source (`input_*|counter|timer|schedule|button.press`).
   - Targetless reloads.
   - `pl == "template"` (`indirect-actuation.md:93-98`).
4. **Expand stored members.**
   - Read configs with `GET /api/config/{domain}/config/<id>`. Scene and script ids come from `config/entity_registry/get`; automation ids from `attributes.id`.
   - Take the union of every branch and skip `enabled:false`.
   - Recognize `action:`, `service:` and `service_template:`, the device form (`domain` + `type`) and the shorthand `scene:` (`indirect-actuation.md:120-156`).
   - `use_blueprint` goes through `blueprint/substitute`.
   - A cycle is resolved when every node in it was read.
   - **Unresolved means typed:** a `pl:template` stored action not read in v1, user-authored `esphome` or `mqtt` buttons, `python_script|shell_command|rest_command`, templated action names, templated targets on access-capable services, a failed substitution, stored `floor_id|label_id`, YAML items without an id, any failed scan, any limit cut.
   - An unread scene on an integration platform is preview with a coverage gap, unless that platform owns a lock, alarm or access-cover entity in the registry; then it is typed (`indirect-actuation.md:296-310`).
5. **Consumer scan**, for every acted entity, including ordinary ones.
   - The scan set is every acted entity plus every group containing one (from `search/related`'s `group` list and from group states already read) and every expanded direct group target; the frozen payload stays leaves-only.
   - (a) Run `search/related` on `item_type:"entity"`, keep automations whose `triggers` subtree contains the entity id (a string match, which also catches literal template and event-data references), read those configs, expand their actions as in step 4, and classify them.
   - (b) Run `GET /api/events` once. If the `call_service` listener count is above the verified baseline, or the target is a `timer`, scan all automation configs (event triggers, `event_data` filters). If enumerated listeners ≠ the live count, the run is typed (`indirect-actuation.md:197-215`). Exception: a `call_service` surplus (live > enumerated automation listeners) is preview with `CALL_SERVICE_LISTENER_UNEXPLAINED`, because an open logbook stream or a raw event subscription adds such a listener (measured on HA 2026.10.1: a stock install has no `call_service` row, so the baseline is 0); listeners the scan attributes to automations keep their own class. Every script run and automation run (direct, stored, or consumer) also fires `script_started`/`automation_triggered`; their listeners are checked the same way, an unexplained surplus being preview with `RUN_EVENT_LISTENER_UNEXPLAINED`. "Unexplained" requires that every enabled automation was read: an enabled automation without id (`YAML_NO_ID`), with an id but no stored config (`UNREAD_CONFIG`), or with a templated `event_type` (`UNENUMERABLE_LISTENER`) is typed; a templated `event_data` filter counts as matching.
   - (c) Check for consumer managers with `config_entries/get` once, plus `hassio` update entities by known slug. If one is present, the run is preview (see Q2).
   - (d) Any `pl:template` entity is a coverage note only (`indirect-actuation.md:228-233`).
6. **Tier** = the maximum over the direct class, member classes, consumer classes, unresolved items (typed), expansion (preview) and gaps (preview).

Caching and limits. All reads go through one `relaySession` per invocation: transport resolved once, request results memoized in memory, 8 concurrent requests, 15 s per request, 30 s overall. There is no disk cache: presence must be checked per run (`indirect-actuation.md:225`). Limits: 64 acted entities, 200 visited nodes, depth 8, 100 consumer configs, 2,000 automations in the full scan. Any cut produces `LIMIT_CUT`, which is typed. This needs a new helper because `relayWSJSONContext` reloads config and reselects transport on every call (`cli/trace_loader.go:197-212`), and `runRelayProxy` is monolithic (`cli/relay.go:341-516`). `trace` then switches to the session helper.

Reason codes (`cli/tier_rules.go`; `TestTierReasonCodesAreDocumented` pins each one here). Section names refer to `skills/ha-nova/indirect-actuation.md` unless another file is named.

| Code | Tier | Rule |
|---|---|---|
| `ACCESS_LOCK` | typed | `lock.unlock`/`lock.open`, directly or as a member (Classifying what you found) |
| `ACCESS_ALARM` | typed | `alarm_disarm` (Classifying what you found) |
| `ACCESS_COVER` | typed | open-capable cover service on a garage/gate/door `device_class` (When you cannot see the members, cover paragraph) |
| `COVER_CLASS_UNKNOWN` | typed | the same on a cover without `device_class` (Q3) |
| `DISRUPTIVE` | preview | restart/stop, `siren.turn_on`/`toggle` (also through `homeassistant.*`), `alarm_trigger` (`service-call/SKILL.md` → Safety) |
| `DISRUPTIVE` (button) | preview | `button.press`, direct or stored, on a button with `device_class` `restart`/`update`, or whose entity_id/name has a whole token restart, reboot, shutdown, poweroff, reset, or the pair shut+down / power+off (PR4 review; `buttonPressTier`) |
| `DESTRUCTIVE_BUTTON` | typed | `button.press` whose entity_id/name has a whole token factory, wipe, or erase — a factory reset wipes the device's configuration (PR4 codex review) |
| `OWNED_SERVICE` | refused | the deferral table names another skill (`service-call/SKILL.md` → Owning-Skill Deferrals) |
| `OWNED_MEMBER` | preview | a stored member is a deferred service (expanding must not downgrade the owner's gate) |
| `OWNED_TYPED_MEMBER` | typed | a stored `mqtt.publish` or `hassio.*` member (owning-skill escalation) |
| `HASSIO_NOT_SUPPORTED` | refused | any direct `hassio.*` (non-goal v1) |
| `UNSUPPORTED_SELECTOR` | refused | direct `floor_id`/`label_id` |
| `CODE_REQUIRED` | refused | code-bearing lock/alarm or a `code` in the payload (`service-call/SKILL.md` → Alarm Panels And Locks) |
| `UNSUPPORTED_FEATURE` | refused | missing `supported_features` bit (same section) |
| `EXPANDED_TARGET` | preview | area, device, or group expansion (I5) |
| `EXPLICIT_BATCH` | preview | more than 10 explicit ids (§4) |
| `STORED_MEMBERS` | preview | the direct target is a scene, script, or `automation.trigger` run (I5) |
| `NO_REGISTRY_ROW` | preview or typed | explicit id without registry row (§2.2 step 1) |
| `TEMPLATE_ACTION_UNREAD` | typed | `pl: template` target; its stored action is not read in v1 (What triggers the gate) |
| `USER_AUTHORED_BUTTON` | typed | `esphome`/`mqtt` button press (Expanding the members, buttons) |
| `UNREAD_COMMAND` | typed | `python_script`, `shell_command`, `rest_command`, `pyscript`, `netdaemon` services; any `command_line`, `rest`, `telnet`, `tcp`, `serial` entity (Expanding the members) |
| `TEMPLATED_ACTION` | typed | templated action name or `service_template` (When you cannot see the members) |
| `UNRESOLVED_TARGET` | typed | templated, `all`/`none`, or non-literal stored target (rule 3) |
| `STORED_SELECTOR` | typed | stored `floor_id`/`label_id` (Expanding the members) |
| `BLUEPRINT_UNRESOLVED` | typed | failed `blueprint/substitute` (Expanding the members) |
| `YAML_NO_ID` | typed | YAML item without an id (When you cannot see the members) |
| `UNREAD_CONFIG` | typed | user-authored config not readable (rule 3; unread cycle members) |
| `INTEGRATION_SCENE_ACCESS` | typed | unread integration scene whose platform owns a lock/alarm/access cover (When you cannot see the members) |
| `INTEGRATION_SCENE_UNREAD` | preview | unread integration scene on a platform without access entities (same section) |
| `RELOAD_UNENUMERATED` | typed | any `<domain>.reload` or `homeassistant.reload_*`; v1 proves no reload side-effect-free (Expanding the members, reloads) |
| `UNENUMERABLE_LISTENER` | typed | live listener count ≠ enumerated automation listeners (step 5b) |
| `CALL_SERVICE_LISTENER_UNEXPLAINED` | preview | `call_service` listeners beyond the enumerated automations (frontend streams; step 5b) |
| `RUN_EVENT_LISTENER_UNEXPLAINED` | preview | `script_started`/`automation_triggered` listeners beyond the enumerated automations (step 5b) |
| `DEVICE_ACCESS` | typed | integration service on an entity whose device has a lock, alarm panel, or access cover (step 2) |
| `CONFIG_WRITE` | preview | `plan write` (create or update); typed when the config's actions reach a typed action (PR5 notes) |
| `CONFIG_REVERT` | preview | `plan revert` of the last verified update (PR5 notes) |
| `CONFIG_DELETE` | typed | `plan delete`, always (PR5 notes) |
| `TARGET_REQUIRED` | refused | a service whose `/api/services` entry takes a `target`, called without one, would act on every entity of its domain; unreadable list → refused for any entity domain (PR3 notes) |
| `RESPONSE_ONLY_SERVICE` | refused | a service that only runs with `return_response` (PR3 notes) |
| `SWITCH_AS_X_ACCESS` | typed | the switch behind an access-capable `switch_as_x` wrapper (step 2) |
| `CONSUMER_MANAGER_PRESENT` | preview | Node-RED/AppDaemon/pyscript present locally (step 5c, Q2) |
| `SCAN_FAILED` | typed | any failed read inside the traversal (Limits you imposed) |
| `LIMIT_CUT` | typed | any budget cut (Limits you imposed) |
| `TEMPLATES_NOT_CHECKABLE` | note | the instance has template entities (step 5d) |
| `NO_LOCAL_CONSUMER_MANAGER` | note | presence check found none locally (step 5c) |
| `INTEGRATION_SERVICE` | preview | service of a non-core domain, targetless or on a device without access entities (step 2) |
| `USER_AUTHORED_SERVICE` | typed | `esphome.*` API service defined in device YAML (rules 2 and 3) |
| `EXPANDED_TARGET_WITHOUT_STATE` | note | an area/device/group leaf has no state (disabled or not loaded) and is not frozen (step 1) |

### 2.3 Execute and verify

**Service calls.**
- Before the POST, take one best-effort snapshot of `config_entries/flow/progress` with `context.source=="reauth"`. On an upstream 500, re-read it and report `reauth_flows_opened` (`service-call/SKILL.md:239-272`).
- Exactly one `POST /api/services/<d>/<s>` with the frozen ids (I7).
- Verification polls `/api/states` with an expect table per domain and service. Transitional states (`opening`, `locking`, …) are waited out: lights up to 3 s, covers, locks and alarms up to 10 s, polling every 500 ms (`service-call/SKILL.md:130,228`).
- Stateless targets (scene, script, button) report `accepted`, except that a scene with read members is `verified` only when every member probe matches (`service-call/SKILL.md:131-132`).
- An unknown service reports `accepted`, never `verified`.

**Config writes.**
- `POST` the config, reload the domain, read it back, and compare `renderConfigChanges(draft, readback)`. `verified` requires both an empty diff **and** a successful reload; a failed or falsy reload reports `unverified` (persisted, not yet running) even when the read-back matches.
- Match the registry `unique_id` to find the real `entity_id`, then confirm the state is present (`relay-api.md:160-168`).
- Updates save to the undo stack automatically (`saveUndoSnapshotBytes`, `cli/snapshot_store.go:92`; the receipt goes into the output).
- Deletes capture the pre-delete config snapshot through Relay `/backups` (`config-snapshots.md`), then run the consumer check on their own `entity_id` (`write/SKILL.md:99`), and are typed.

## 3. Prose replaced → skill shrink (words, rough)

| File | Now | After | Replaced by |
|---|---:|---:|---|
| `hooks/session-start` injection | 6,828 | ~600 | Inject a compact index (mission, safety core, dispatch rows only). The full router moves on demand. |
| `skills/ha-nova/SKILL.md` | 5,652 | ~2,600 | Quoting Reliability (`:49-67`) only for raw-relay routes; Confirmation Tiers (`:95-104`) point to CLI `tier`; dispatch examples deduplicated |
| `skills/ha-nova/session-bootstrap.md` | 1,176 | ~250 | `notices[]` on every verb |
| `skills/ha-nova/output-rules.md` | 2,636 | ~700 core + cards on demand | Verb routes carry a 3-line result rule |
| `skills/service-call/SKILL.md` | 5,201 | ~1,600 | Flow 1–6 (`:86-135`), state-delta rules, alarm/lock gates (`:208-228`), reauth (`:239-272`) and helper patterns move to `call`/`apply`. Deferral table, events/webhooks and response services stay. |
| `skills/ha-nova/indirect-actuation.md` | 3,629 | ~3,600, off verb routes | Becomes the engine's spec. Loaded only by the prose routes (events, assist, scene test). Reason codes are pinned to its sections by a contract test. |
| `membership-resolution.md` / `consumer-discovery-preflight.md` / `outcome-verification.md` | 933 / 536 / 874 | off call route | CLI expansion, scan, verification |
| `skills/entity-discovery/SKILL.md` | 2,439 | ~1,300 | Steps 1–2, jq recreate blocks, alias, diacritics and truncation rules move to `find`/`get`. State-snapshot semantics stay. |
| `skills/ha-nova/relay-api.md` | 4,217 | ~2,900, off call/write mandatory lists | ID resolution (`:131-158`), post-write verification (`:160-168`), consumer jq (`:195-199`) |
| `skills/write/SKILL.md` | 2,727 | ~1,500 | Phase 1 resolve, 3c impact, Phase 3 apply, Phase 4 step 1 read-back, snapshot save → `plan write` + `apply` |
| `agents/resolve-agent.md` + `apply-agent.md` | 1,871 | 0 | deleted |
| `skills/ha-nova/write-safety.md` | 3,330 | ~1,900 | Drift check (`:67-82`), diff invocation, snapshot mechanics. Behavior narrative and honesty wording stay. |
| `skills/ha-nova/update-revert.md` | 494 | ~150 | `plan revert` |
| `*.jq` assets | — | kept | until the last raw-relay consumer migrates |
| `skills/review/checks.md` | 7,901 | unchanged | Optional split into a pre-write core (~3,500) only if the write route misses 25k tokens live |

Route totals after the change:
- Service-call: ~600 + 1,600 + 250 ≈ 2.5k words (≈ 3.4k tokens).
- Write: ~15.4k words (≈ 20.8k tokens) before tool I/O.

The Safety Core needs amendments, and they are linter-enforced byte for byte (`docs/reference/skill-architecture.md:611-621`, `tests/skills/skill-template-contract.test.ts`):
- Bullet 1 becomes "Preview before write, except the CLI fast path (`tier: ordinary`)".
- Bullet 6 becomes "through the `ha-nova` CLI".
- `write/SKILL.md:180` ("no MCP") must be reworded.

## 4. Fast-path decision table

| Situation (as computed by the CLI) | Tier | Behaviour | Example |
|---|---|---|---|
| One exact id or unique `--match`, ordinary action, no consumer or only ordinary fully-read consumers, coverage notes only | ordinary | execute + verify, no preview | "turn off the kitchen light", "set the hallway thermostat to 21", "lock the front door", "close the garage door" |
| Explicitly listed ids (≤ 10), each meeting the row above | ordinary | one call, per-member verification | "turn off kitchen and hallway light" |
| Helper / trigger-source write whose consumers are all read and ordinary | ordinary | execute; consumers listed in the result | "turn on guest mode" (an automation answers with lights) |
| Area / device / group / stored-member run (scene, script, `automation.trigger`) | preview | exit 3 → user "yes" → `apply` | "lights off in the kitchen", "activate Movie Night", "run the bedtime script" |
| Disruptive service; unread integration-owned member; consumer manager present (Q2) | preview | exit 3 → natural confirm → `apply` | "restart Home Assistant", Hue scene |
| Config create/update/revert | preview | `plan write` shows `diff` → `apply` | "create an automation …" |
| Ambiguous or zero match | — | exit 5, one question | "turn off the light" (12 lights) |
| Unlock/open lock, alarm disarm, access-cover open (or cover without `device_class`), any member or consumer reaching these, any unresolved user-authored item or limit cut, delete | typed | exit 3 with `code` → user types it → `apply --confirm` | "unlock the front door", "run leave-home" (script unlocks), "delete automation X" |
| Owned service, floor/label selector, code-required alarm/lock, missing feature bit | refused | exit 4 + owner/handoff | `mqtt.publish` → `ha-nova:mqtt` |

## 5. MCP adapter: `ha-nova mcp`

- **Transport.** stdio, newline-delimited JSON-RPC 2.0. Methods: `initialize`, `tools/list|call`, `prompts/list|get`, `resources/list|read`. Hand-rolled on stdlib, about 400 LOC; this follows the dependency rule (`AGENTS.md:13`). The official Go SDK is the fallback if the protocol surface grows.
- **In-process.** Tools call the same verb functions. No subprocess.
- **Auth.** The server reuses `loadConfig` + `selectRelayTransport` (paired device credential from the OS store, `cli/relay_transport.go:75-112`). No new credential. A locked keyring returns a tool error with `ha-nova setup`, and no secure-storage UI is shown (`docs/reference/safety.md` ForbidUI row).
- **Server selection.** `ha-nova mcp --server <name>` or the `HA_NOVA_SERVER` env var in the client config.

| Tool | Verb | readOnlyHint | destructiveHint | idempotentHint |
|---|---|---|---|---|
| `ha_find` | find | true | false | true |
| `ha_get` | get | true | false | true |
| `ha_history` | history | true | false | true |
| `ha_trace` | trace | true | false | true |
| `ha_read` | raw `core GET` / `ws` from the read allowlist (same as I1) | true | false | true |
| `ha_plan` | plan `call\|write\|delete\|revert\|raw` (raw writes: tier ≥ preview, DELETE or `*/delete` = typed) | true | false | false |
| `ha_call` | call | false | false | false |
| `ha_apply` | apply | false | true | false |

- **Typed confirmation over MCP (two steps).** `ha_call` or `ha_plan` returns `status: needs_confirmation`, a text preview and `code`. The tool-result text says: show the preview, ask the user to type the code, then call `ha_apply` with `plan_id` and the user's exact `confirm_code`. `ha_apply` checks it exactly as the CLI does (§2.1). The `initialize.instructions` field (~150 words) carries the safety core, including "never pass a code the user did not type".
- **Prompts and resources.** Prompts expose `ha-nova` plus each `skills/<name>/SKILL.md` as `ha-nova-<name>`, read from the installed bundle via `resolveSourceRoot` (`cli/install_source.go:44`). Each prompt gets an ~80-word preamble that maps `ha-nova <verb>` to its tool, and `ha-nova relay …` to `ha_read` or `ha_plan kind=raw`. Resources expose `ha-nova://skills/<path>.md` for shared references, read on demand.
- **Out of scope for v1.** HTTP, SSE and streamable transports; remote access; MCP OAuth; elicitation (a later option for typing the code in the client UI); sampling; subscriptions; writing client MCP configs automatically (docs carry the JSON snippet instead).

## 6. Delivery: stacked PRs (≤ ~800 prod LOC each)

Every PR is release-bound because it contains Go code. PR2, PR3 and PR5 are high-risk (`AGENTS.md:18`) and get adversarial subagents before opening (`AGENTS.md:46`).

**PR1 (~650 LOC): `relaySession` + envelope + `find` / `get` / `history`, plus `notices`.**
- Oracle: Go tests with the existing fake relay (`setupTraceRelayTest`, `cli/trace_test.go:186`):
  - truncation counts are exact
  - alias fallback runs only after a compact-registry miss
  - diacritic folding
  - `get --config` never resolves by slug
  - I1 allowlist asserted
- E2E: `scripts/e2e/disposable-ha/run.sh` writes `demo:` into the fresh config (`run.sh:41-43` currently starts empty), builds the CLI, configures it with `setup --service --relay-url --relay-token` (`cli/command_setup.go:34-35`), and asserts that `ha-nova find "bed light"` returns `light.bed_light`.

**PR2 (~800 LOC): tier engine + `plan call` (read-only).**
- Oracle: table-driven golden cases, one per rule in `indirect-actuation.md`:
  - template light
  - disabled unlock step
  - device-form unlock
  - shorthand scene
  - `use_blueprint`
  - read cycle vs unread cycle
  - depth cut
  - `call_service` listener surplus
  - garage `set_cover_position: 10`
  - integration scene on a lock-owning platform
  - YAML automation without id
- A contract test pins every reason code to a section heading of that doc.
- E2E: on the disposable HA, measure the baseline `call_service` listener count. This is the blocking verification for §2.2 step 5b.

**PR3 (~600 LOC): `call` + `apply` for services (store, hash, code, expiry, drift, verify, reauth, exit codes).**
- Oracle: Go tests for I2, I3, I6, I7 (one POST even on timeout) and I8 (including two concurrent `apply` processes on one plan: exactly one sends).
- E2E, demo platform:
  - `call light.turn_off light.bed_light` gives exit 0 `verified` in one invocation
  - `call lock.unlock lock.front_door` gives exit 3 typed
  - `apply` with a wrong code gives exit 4
  - a state change between plan and apply gives exit 2
  - the right code gives `verified`

**PR4 (skills + hook; ~50 LOC Go): migrate service-call and entity-discovery.**
- Session-start index, bootstrap via notices, output-rules split, Safety Core amendment, `safety.md` rows.
- Oracle: add a `service-call` row to `route-context-budget-contract.test.ts` (≤ 3,000 words); skill-template test updated.
- Behavioral e2e: a Codex scenario "turn off the bed light" in `scripts/e2e/codex-ha-nova-scenarios.json` must contain `ha-nova call` and forbid `relay core`.

**PR5 (~700 LOC): `plan write|delete|revert` + `apply` config path.**
- Oracle: Go tests for drift on an external edit (normalized compare), read-back mismatch reported as `unverified`, an undo receipt on update, a pre-delete snapshot, and delete typed-only.
- E2E: create an automation, edit it through raw relay, then `apply` the stale plan gives exit 2.

**PR6 (skills only): migrate write.**
- Delete the agents, shrink `write-safety.md` and `update-revert.md`, lower the write ratchet to ≤ 15,500. Add the `checks.md` split only if live runs exceed 25k tokens.
- Oracle: ratchet plus the existing write contract tests.

**PR7 (~600 LOC): `ha-nova mcp`.**
- Oracle: a Go test drives the server over pipes: `initialize` → `tools/list` (exact annotations) → `ha_call` typed → `ha_apply` with a wrong code, then the right code, against the fake relay.
- E2E: a scripted stdio client in `run.sh` calls `ha_call` on `light.bed_light` and expects `verified`.

**Backward compatibility.**
- `ha-nova relay health|ws|core|files|backups|jq|version` is untouched (`cli/relay.go:75-110`), so unmigrated skills keep working through every PR.
- The `.jq` assets stay until no skill references them.
- Verbs carry `schema_version` and `--help`.
- Skills and CLI ship in one bundle, so no skew rule is needed beyond dev-sync.
- The `relay` line stays in `printUsage` (`cli/main.go:207`).

## 7. Risks and open questions

Risks
- **The tier engine becomes a single point of truth.** Mitigations: fail-closed defaults, golden cases per prose rule, reason codes pinned to the doc, adversarial review.
- **Wrong target on the fast path through `--match`.** Mitigations: unique match within the domain only, the result echoes the resolved name, ambiguity exits 5.
- **The model can confirm a code on its own** (§2.1). The documented gate is a client permission rule on `apply`; elicitation stays a later option.
- **Latency over the Cloud transport** when the full automation scan triggers. Bounded at 30 s; a cut goes typed.
- **v1 UX regressions.** `pl:template` targets go typed, and Hue-style scenes go preview.
- **Known limit: unfiltered `state_changed` listeners.** An automation with an event trigger on `state_changed` (no entity filter) consumes every state change, but neither `search/related` nor the per-type listener check discovers it: core keeps its own `state_changed` listeners, so no baseline separates them. The prose has the same gap. A plan does not see such an automation.
- **Known limit: physically wired openers.** A plain switch or relay wired to a door opener, with no `switch_as_x` wrapper or access `device_class`, is indistinguishable from a lamp switch and stays ordinary.
- **Public guarantees change.** `docs/reference/safety.md:16` ("Nothing is written before you see a preview"), README, and the conventions in `AGENTS.md:100` and `PROJECT.md:58-59`. The README change goes only into the release-prep PR (`AGENTS.md:96`); the "What To Watch" note goes into `docs/work/next-release-body.md`.

Decisions (maintainer, 2026-10-14)
- **Q1 → yes.** Consumer hits whose actions are fully read and ordinary keep the fast path; the result lists them.
- **Q2 → preview.** A present but unreadable consumer manager (Node-RED, AppDaemon) puts ordinary device control at preview, with a coverage note.
- **Q3 → typed.** An open-capable cover without `device_class` is typed (fail closed).
- **Call-service surplus → preview** (maintainer default 2026-10-14, analogous to Q2). An unexplained `call_service` listener surplus is preview with `CALL_SERVICE_LISTENER_UNEXPLAINED`; a surplus listener the full scan attributes to an automation reaching a typed action stays typed.
- **ESPHome/MQTT entities stay ordinary** (maintainer default 2026-10-14). Only their buttons are user-authored typed; switches, lights, and covers are real devices, and escalating them would put every zigbee2mqtt switch on typed.
- **Q4 → change with an explicit exception.** Public wording: "Unambiguous everyday actions run immediately and are verified afterwards; everything else shows a preview first; locks, gates, alarm, and deletes need a code." `docs/reference/safety.md` and the Safety Core change with PR4; README only in the release-prep PR.

PR5 implementation notes
- `plan write --domain automation|script [--id] --file` keeps the draft's bytes exactly (whitespace removed, key order untouched) — Home Assistant evaluates `variables`, `trigger_variables`, and script `fields` top-down, so the POSTed body is the body given; a revert restores the checkpoint's bytes the same way. Ids: automations take `--id`, the draft's `id`, or a minted millisecond id like the HA UI (must be unused); scripts take `--id` or a key derived from the alias (`^[a-z0-9_]+$`), never a minted number, and a script draft carries no `id`. A 404 config whose id a YAML/package item owns in the registry is refused (`CONFIG_NOT_UI_MANAGED`), never created over. The plan carries `diff` (`renderConfigChanges(current, draft)` plus order rows for those maps) and walks the draft's actions with the tier engine, so a write that makes an automation unlock a door is typed.
- `plan delete <automation.x|script.x>` resolves the config id from the entity (never the slug), is always typed, and lists what references the entity (`search/related` automation/script/scene) as consumers.
- `plan revert [--target] [--domain]` uses this server's checkpoints (records carry `server`; untagged legacy records stay eligible, the drift check compares them with this server's live config); automation/script `update` checkpoints only. When the live config no longer matches the stored post-write read-back (`snapshotMatchesLive`), it plans nothing and exits 2 (`REVERT_DRIFT`).
- The action hash covers op, domain, config id, endpoint path, and body; the basis holds the normalized live-config digest plus the key order of order-semantic maps (`absent` on create), the draft digest, member config digests, and acted entities. Cosmetic key order elsewhere and HA bookkeeping keys never drift; a reorder of `variables`/`fields` does.
- `apply` keeps the call rules (claim, re-validation, recompute, code from the recomputed plan, single use). Execution: (delete only) a pre-delete snapshot of the raw live config through Relay `/backups` (`auto-<entity-slug>`); a failed snapshot deletes nothing, marks the plan, and hands it back — only then is `--no-snapshot` accepted (exit 4 otherwise). Then exactly one `POST`/`DELETE /api/config/<domain>/config/<id>`, one reload (`automation.reload` scoped to `{"id": <config id>}`, as Home Assistant's own config hook does; `script.reload` in full), and the read-back. A write is `verified` only when the read-back equals the body (content and semantic order), the reload barrier succeeded, and the registry `unique_id` maps to an entity that is not `unavailable`; a delete when config and entity are gone (a delete answered 400/404 because the item was already gone is verified with a note).
- Checkpoints follow `update-revert.md`: an update — a revert included — saves its checkpoint on any 2xx write and on an unknown outcome (expected-after = the read-back, else the body), and the output carries `before_config` whenever the update may have landed. So a saved-but-unverified update stays revertible, and a second revert reverts the revert.
- The write allowlist (`checkWriteRequest`) grows to exactly these: config `POST`/`DELETE` for automation/script, service `POST` (reload included), and a `/backups` `save` in categories `automations`/`scripts` with an `auto-` name. Read verbs keep I1.
- One comparator (`configChanges`: normalized content plus the order of `variables`/`trigger_variables`/`fields`) serves the plan diff, the read-back check, the revert guard (`snapshotMatchesLive`), `ha-nova snapshot verify`, and `ha-nova diff`; the drift digest hashes the same two parts.
- Registry-disabled automations/scripts have no state: `plan delete` resolves their config id from the registry `unique_id`, and a write to one verifies on the read-back plus the registry row (noted as disabled, not running).
- `ha-nova snapshot show|verify` select by server like `plan revert` (`--server`, else the default profile; untagged legacy records stay eligible, newest per domain+target wins), and `--list` shows each record's server.

PR3 implementation notes
- `call` executes only ordinary plans and never stores them; every other tier is stored and exits 3. `apply` on a preview plan is the confirmation step itself (the skill obtains the natural confirmation first); typed plans need `--confirm`, a missing code is refused without counting as a mismatch.
- The only write is `relaySession.postService` (core `POST /api/services/<d>/<s>`, 30 s, no retry); `read()` keeps refusing it (I1). Relay 4xx before upstream → exit 1 (nothing sent); transport errors, redirects, Relay 5xx, unreadable replies → exit 7 with status `unverified` and `outcome_unknown: true` (the status stays in the verified|accepted|unverified|failed vocabulary). Upstream HA 400/404/422 (proven rejection) → exit 6 `failed`, no probes; any other upstream ≥ 400 (e.g. 500) → probes run, exit 6 `unverified`; a 500 adds `reauth_flows_opened` — only flows of a target's config entry, or of its integration when that has exactly one entry — from one re-read 2 s later. The pre-call reauth snapshot is bounded to 5 s.
- Apply order: peek (server only) → session → atomic claim → kind/version/request re-validation → expiry/server/empty-target guard → recompute (exit 2 stores the new plan, the claimed file is deleted) → id/tier/code from the recomputed plan → one POST → verify → delete. A claimed plan is never reused; a wrong code below three attempts releases it.
- Response-only services (`calendar.get_events`, `todo.get_items`, `weather.get_forecasts`, and any service `/api/services` lists with `response.optional: false`) are refused (`RESPONSE_ONLY_SERVICE`); a failed `/api/services` read refuses nothing, because Home Assistant itself rejects such a call (400) before running it.
- Targetless calls: a service whose `/api/services` entry has a `target` is refused without named targets (`TARGET_REQUIRED`, exit 4, nothing stored); if the list cannot be read, any service of an entity domain is refused (except `<domain>.reload`, `script.<id>`, and `scene.apply`). Genuinely targetless services keep their tiering. A request that named targets but froze none is refused on both `call` and `apply` (`UNRESOLVED_TARGET`).
- Fields whose value is the entity state (`option` on select/input_select, `value` on number/input_number/text/input_text, `hvac_mode` on climate) are read from the state — one function for verification, the drift basis, and deltas — so a change during confirmation drifts (exit 2).
- A Relay that never answers in the invocation (connection refused, DNS, TLS, timeout before any reply) is an operational error: exit 1 `RELAY_ERROR` with the usual connect or Cloud wording, nothing planned or stored, and `apply` releases its claim. `SCAN_FAILED` (typed) stays for failures after the Relay answered at least once, and for upstream Home Assistant errors.
- Verification renews the read budget after the POST and bounds every read by what is left of its window. Probes check the state and every data field the call sets (brightness within rounding, color temperature, percentage, positions, temperatures, modes, select/number/text values, `rgb_color`); text compares literally, numbers numerically. A field without a probe downgrades the target to `accepted`. Scenes probe every member reached through the Via chain (groups included) with the stored state and its known attributes. A cover, valve, lock, or alarm panel still in a transitional state when its window closes is `accepted` with a note.

PR2 implementation defaults (fail closed; open for maintainer review)
- `call_service` baseline is the named constant `callServiceListenerBaseline` = 0 until the disposable-HA E2E measures it. A core listener that is never enumerated then reads as `UNENUMERABLE_LISTENER` (typed).
- Any non-literal stored target (templated, `all`/`none`) is typed, not only on access-capable services (rule 3).
- Every reload is typed (`RELOAD_UNENUMERATED`); v1 does not enumerate any reload's effect set.
- `alarm_trigger` on an alarm panel stays preview (disruptive, grants no access) inside the access allowlist.
- A trigger targeted by `area_id`/`floor_id`/`label_id`/`device_id` (or a templated target) counts as a consumer match.
- A failed read during target resolution is a typed gap (exit 3); a selector that resolves to nothing because its read failed is `UNRESOLVED_TARGET`, and PR3 `apply` must never execute a plan whose request named targets but froze none.
- A present consumer manager is preview for every target, trigger sources included (Q2 wording), although the prose escalates trigger sources further.
- `call_service` listeners ignore `event_data` filters: every enumerated listener counts as a consumer of the run (over-approximation).
