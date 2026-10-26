---
name: integration-setup
description: Use when adding, removing, configuring, reloading, enabling, disabling, or renaming Home Assistant integrations, managing their subentries, ignoring discoveries, or recovering invalid integration credentials when no reauth flow is pending — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Integration Setup

## Scope

Integration entry lifecycle, standard config/options flows, subentries, discoveries, pending reauthentication (`reauth`), and Credential Recovery when no reauth is pending.

Not handled here:

- helper config-entry flows (use `ha-nova:helper`)
- YAML-only integrations (use `ha-nova:yaml-config`)
- diagnosing setup failures after a flow finishes (use `ha-nova:diagnose`)

Credential-bearing, external/OAuth, and progress steps finish in the Home Assistant UI; this skill never collects secrets in chat.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use response-driven flows. Writes use `ha-nova plan raw --file <requests.json>`, then `ha-nova apply` per `next_step`; never bypass a refusal. Flow GETs can advance state: plan them too.

- reads: `ha-nova relay core --method GET --path /api/config/config_entries/flow_handlers`; `ha-nova relay ws --data-file <payload-file> --out <file>` with `{"type":"manifest/list"}`, `{"type":"config_entries/flow/progress"}`, or `{"type":"config_entries/get"}`
- `/api/config/config_entries/flow[/<flow_id>]`: POST start; GET/POST/DELETE by id.
- `/api/config/config_entries/options/flow[/<flow_id>]`: same methods.
- Subentries: `/api/config/config_entries/subentries/flow[/<flow_id>]` (POST start; GET/POST/DELETE by id).
- reload: `ha-nova call homeassistant.reload_config_entry --set entry_id=<entry_id>` (typed, like every reload)

Raw request: `{"endpoint":"core","payload":{"method":"POST","path":"<path>","body":{...}}}`. GET/DELETE omit body. Relay-core data is under `.data.body`.

## Flow

### Resolve the operation

1. For add, join `flow_handlers` to manifest `domain`; resolve one exact domain or manifest-name match. Ambiguous: ask, never guess.
2. For reauthentication:
   - read `config_entries/get` and resolve the exact existing `entry_id`
   - read `config_entries/flow/progress`
   - select only `context.source == "reauth"` with matching `handler` and `context.entry_id`
   - if no matching pending flow exists: with credentials reported invalid, continue with Credential Recovery below; otherwise report that Home Assistant is not currently requesting reauthentication; never synthesize a reauth flow
3. Limit one integration flow per operation.

### Start or continue

For add:

1. Capture `config_entries/get` as the verification baseline.
2. Preview the exact integration domain and flow start. State that no integration has been added yet.
3. End the turn with this preview for confirmation.
4. POST `{"handler":"<domain>"}` to `/api/config/config_entries/flow`; extract `flow_id` or fail loud.

For reauthentication, GET the matched `/api/config/config_entries/flow/<flow_id>`; do not create a second flow.

### Iterate live steps

This iteration follows the shared `../ha-nova/live-schema-preflight.md` contract, which additionally binds pre-confirmation navigation; preview every start, GET, and submit, even when the terminal boundary is unknown.

Treat every response as authoritative:

- `menu`: returned options only; selection is the bound confirmation for `{"next_step_id":"<choice>"}`. Resolve a bare number: name the selected option before submitting it (Interactive Choices).
- `form`: use only fields returned by the live `data_schema`; preview the full body before submitting. For secrets, never build or submit its body.
- validation errors: show the returned field errors and stop; never guess a replacement.
- `external`, `progress`, or a form requesting a password, PIN/code, OAuth grant, key, token, certificate, or private key: DELETE an agent-started transient flow; restart at **Settings > Devices & services**, using the entry's Configure/Reconfigure or subentry control as appropriate. Preserve pre-existing reauth flows and point at their in-progress card. User-started flows are omitted from `config_entries/flow/progress`; the Relay lacks the frontend-origin header for OAuth redirects. Never ask for or echo the secret; never claim completion.
  For an agent-started OPTIONS or RECONFIGURE flow, DELETE the transient flow; use the entry's Configure control in the UI.
- `create_entry`: verify. A `next_flow` needs its owning operation's fresh preview; unsupported families stop in the UI.
- `abort`: report the returned reason. For reauth, only `reason == "reauth_successful"` is a success result, and only after config-entry verification.

Cancel: DELETE only this skill's unfinished flow. Never delete a pre-existing reauth flow.

### Verify

1. Re-read `config_entries/get`.
2. Add passes when the terminal response's `result.entry_id` exists. If the result omits it, diff the baseline by `entry_id`; exactly one new entry with the requested domain must exist or verification is ambiguous.
3. Reauth passes only when the same `entry_id` still exists, the matching reauth flow is gone, and the terminal result reports success. Report the current config-entry state exactly; do not call a non-`loaded` entry healthy.
4. Linked devices/entities are secondary evidence only.

### Options, Reconfigure, and Reload

Resolve `entry_id` from `config_entries/get`; use the same live-step rules. An integration's OWN API stays with `ha-nova:fallback`.

- **Options** (requires `supports_options: true` on the entry): start with POST `{"handler":"<entry_id>"}` to `/api/config/config_entries/options/flow`, iterate `.../options/flow/<flow_id>`. Read `description.suggested_value` as the current values (one shown as hidden: hand the form to the UI), merge only the requested changes over them, preserve unrelated existing options, and never submit a field the live step does not expose. Verify by reopening the options flow and reading the changed fields back — the terminal response alone is not evidence, and neither is unrelated entity state. Then DELETE the reopened verification flow (same cleanup rule as an unfinished add flow).
- **Reconfigure**: start POST `{"handler":"<domain>","entry_id":"<entry_id>"}` to `/api/config/config_entries/flow` — `entry_id` selects reconfigure mode (a body `source` key is ignored by the API); without `entry_id` Home Assistant starts a normal ADD flow. The first response must be this entry's reconfigure step; integrations without reconfigure support typically fail the flow start — treat any non-reconfigure form (a plain add form included) as unsupported: DELETE the flow and stop. A secret, OAuth `external`, or `progress` step here: DELETE the transient flow and point at the entry's own Configure/Reconfigure control under **Settings > Devices & services**. Verify: the same `entry_id` persists and the before/after `config_entries/get` diff shows NO new entry.
- **Reload**: preview with the disruption disclosure — reload re-runs setup and briefly drops the entry's entities — confirm, run the reload call, then re-read `config_entries/get` and report the entry's ACTUAL state; a `false` result or non-`loaded` entry is the outcome to report, never a success.

### Enable, Disable, Rename, Ignore

One WS item through `ha-nova plan raw --file <requests.json>`, then `ha-nova apply` per its `next_step`; resolve `entry_id` from `config_entries/get` first.

- **Disable/enable**: `{"type":"config_entries/disable","entry_id":"<id>","disabled_by":"user"}` (`null` enables). Preview: disable stops its devices and entities; enable restores them; name consumers (`search/related`). An entry with a lock, alarm panel, or access cover is refused: point at **Settings > Devices & services**.
- **Rename, system options**: `config_entries/update` with requested `title`, `pref_disable_polling`, `pref_disable_new_entities`. Polling changes reload loaded entries: preview the interruption. Same access refusals; `require_restart: true` → explain the pending restart.
- **Ignore a discovery**: a `config_entries/flow/progress` flow with `context.unique_id` → `{"type":"config_entries/ignore_flow","flow_id":"<id>","title":"<name>"}`; the UI's ignored list restores it.

Verify by re-reading `config_entries/get` or the progress list.

### Subentries

Read `supported_subentry_types` from the resolved parent and `{"type":"config_entries/subentries/list","entry_id":"<id>"}` (WS `.data` array). Resolve the exact subentry and type; retain a before-list.

- Add: POST `{"handler":["<entry_id>","<subentry_type>"]}` to `/api/config/config_entries/subentries/flow`.
- Reconfigure: same start plus `"subentry_id":"<id>"`; require that type's `supports_reconfigure`. Iterate its by-id path with the live-step rules; no guessed fields or terminal boundary. Destructive changes require `typed:true`. Hidden current values: UI, not an invented replacement.
- Rename: WS `config_entries/subentries/update` with `entry_id`, `subentry_id`, `title` only.
- Delete: WS `config_entries/subentries/delete` with both ids, typed; use Removal's scoped impact scan first.

The CLI refuses all subentry flow starts and deletes when the parent owns a lock, alarm panel, or access cover; use the UI. Rename remains previewed. Verify through the subentry list: add requires exactly one new matching id; edit requires the same id, no additions, and reopened live values (cancel verification flow); rename matches title; delete proves absence. Parent and unrelated subentries must survive. Missing values or uncertain responses: unverified, never retry blindly.

### Removal

Resolve the entry; read FULL entity/device registries, never compact display rows. Entities match `config_entry_id`; devices follow `../ha-nova/membership-resolution.md` → Device rows (singular ownership, legacy array only when absent). Subentry deletion additionally matches entity `config_subentry_id` and normalized device subentry membership per that contract. Preview all affected counts, interruption, and no auto-undo; offer `ha-nova:backup`.

Run `search/related` for EACH owned device AND entity; validate successful object responses before claiming no consumers. Input devices also require stored automation event-listener scans per `../ha-nova/consumer-discovery-preflight.md`. Disclose unindexed dashboard/template references and incomplete scans before confirmation.

Entry removal: typed raw DELETE `/api/config/config_entries/entry/<entry_id>`. Verify absence in `config_entries/get`, re-read registries, report surviving records and `require_restart` honestly; never restart automatically.

### Credential Recovery (no reauth pending)

Credentials reported invalid, but `config_entries/flow/progress` shows no
matching reauth flow:

1. `loaded` is lifecycle state, never proof the stored credential works — do
   not call an entry healthy because it is `loaded`.
2. Preview and confirm the reload's setup interruption, then `ha-nova call homeassistant.reload_config_entry --set entry_id=<entry_id>`. Preserve the entry and every subentry; never delete or recreate anything.
3. Inspect the reload call's result and re-read `config_entries/get` first: a
   failed call or a non-`loaded` entry means the reload itself did not
   complete — report that actual entry state, never a missing upstream
   trigger. Then re-read `config_entries/flow/progress` either way (an auth
   failure can still open reauth). Reauth flows open asynchronously (often
   only after the first refresh), so wait a few seconds and re-read once
   more before concluding nothing appeared. A new flow with
   `context.source == "reauth"` and the same `entry_id` → continue with the
   normal reauthentication handoff above.
4. Still no flow on a settled, SUCCESSFUL re-read — AND step 3's reload
   check passed (call succeeded, entry `loaded`): Home Assistant exposes no
   supported trigger for this integration — say so plainly and hand off to
   **Settings > Devices & services**. A FAILED re-read is not that evidence:
   report step 3's actual reload result (done only if its check passed) and
   the flow state as unknown, never as unsupported. Never synthesize a config flow, edit `.storage`, create a
   replacement entry, or reach for deprecated integration services as a
   workaround.
5. Verified success is only a terminal `reauth_successful` for the same `entry_id` plus entry verification. UI completion with the matching flow gone on a settled re-read is reported as completed but unverified, never as proven recovery: cancellation looks identical. Do not spend a paid API request to "test" the credential unless the user asks.
6. Secrets and key fragments never appear in previews, output, or logs.

## Error Handling

- Relay/upstream failures follow `../ha-nova/relay-api.md` → Error Handling.
- `404`: reconcile per live-schema preflight; never blindly restart an uncertain submit.

## Output Format

Apply `../ha-nova/output-rules.md`: plain words, HA names verbatim; unless asked or names clash, no ids, "entity", modes, Relay/HA URLs, snapshots, raw JSON.

Use the Preview Card for flow start, menu selection, each non-secret form submit, and every entry reload (credential-recovery reload included). Results name the integration, operation, config-entry state, and exact verification scope. UI handoffs state what remains and never display secret fields or values.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing speculative.

- Declared exception to the core delete rule above: canceling this skill's unfinished add/options/reconfigure/subentry flow removes only ephemeral state. Explicit cancel or required UI restart suffices, never for an entry or subentry.
- Never request, echo, persist, or submit credentials from chat; finish credential-bearing steps in the Home Assistant UI.

## Guardrails

- Live `data_schema` and menu options are the only accepted field source.

## References

- Relay API: `../ha-nova/relay-api.md`
- Shared write safety: `../ha-nova/write-safety.md`
