# Confirmation hardening: typed-tier actions need the human (2026-10-17)

Status: active

Implemented on `fix/close-confirmation-bypasses`. Maintainer decision 2026-10-17: the paths around a typed plan's confirmation must not work; breaking the raw service, event, and webhook routes is approved.

Inputs: `docs/internal/2026-10-17-human-confirmation-channels.md` (P0.1–P0.3, reproduced on a throwaway HA), `docs/work/2026-10-14-typed-verbs-spec.md` §2.1, `docs/work/2026-10-16-mcp-human-confirmation-spec.md`.

## 1. Threat model

Assets: typed-tier actions (`confirm: code`): unlock/open a lock, open an access cover, disarm, destructive buttons, `plan delete`, every reload and restart, and every other reason the tier engine marks typed.

Actors, as in the channels report:
- **T1** a cooperative model that slips. **T2** a prompt-injected model that uses the commands the skills document and the files it can read. **T3** an adversarial model with a shell, the same OS user, and the will to read the OS credential store or attach to processes.
- Goal of this change: no T1/T2 path runs a typed action without the human; T3 is stated, not solved.

### 1.1 Existing credential storage (the key's home)

The Relay credential is the device credential after pairing (or the legacy Relay token): macOS login keychain, Linux Secret Service (or a 0600 service token file), Windows Credential Manager, or the insecure test file.

| Store | Can a same-user process get the credential without the human? |
|---|---|
| macOS keychain item created by `ha-nova` | A direct reader (`security`, another binary) is prompted by the ACL. But `ha-nova` itself reads it with no prompt, and a model can make it hand the credential over: point `relay_secure_base_url`/`relay_spki_pin` (or `relay_base_url`) in the user-writable `config.json` at a listener the model controls and run any `ha-nova relay` command — `ha-nova` sends `Authorization: Bearer <credential>` to it; or run `pair --credential-store=file`, which migrates the credential to a 0600 file on macOS too, then abort and read the file. So: yes, on macOS as well, through `ha-nova`. |
| Linux Secret Service | Yes, once the collection is unlocked (no per-app ACL). |
| Linux / macOS token or credential file (`--service`, `--credential-store=file`, containers) | Yes: 0600 but owned by the same user. |
| Windows Credential Manager | Yes (no per-app ACL). |

Whoever can read this credential can also call the Relay (or Home Assistant) directly and needs no code at all. So a key derived from it is exactly as strong as the credential it protects, never weaker — and on **every** platform a model with a shell as the same user can reach the credential (directly on Linux/Windows/containers/files, or via `ha-nova` on macOS). The code is therefore T1/T2 protection everywhere and never T3. Closing T3 needs the credential bound to the pinned endpoint and a human prompt before the keychain→file migration (follow-up, not in this change) — or, the real fix, an HA-side gate the agent never holds.

## 2. Bypasses and fixes

| # | Bypass (before) | Fix (after) | Regression test |
|---|---|---|---|
| B1 | `ha-nova relay core --method POST --path /api/services/<domain>/<service>` (and WS `call_service`, `execute_script`) runs any service, a typed one too, with no plan | `runRelayProxy` refuses it before anything is sent (`cli/relay_raw_guard.go`); the refusal names `ha-nova call` | `TestRawRelayRefusesServiceCalls` |
| B2 | Raw event fire (`POST /api/events/*`, WS `fire_event`) and webhooks (`/api/webhook/*` any method, WS `webhook/*`) start automations with no plan | Refused; the next step is `ha-nova call automation.trigger <automation>` (tiered by what it runs) or the Home Assistant UI | `TestRawRelayRefusesEventsAndWebhooks` |
| B3 | Other raw actuation surfaces: `POST /api/states/*` (state-triggered automations), `/api/intent/*`, `/api/conversation/*`, WS `conversation/process`, `assist_pipeline/run`, `/api/hassio/*` writes, WS `supervisor/api` writes, WS `backup/restore`, smart-home fulfillment (`/api/google_assistant`, `/api/alexa`), `/api/mobile_app/*`, `/api/cloud/*` writes | Refused | `TestRawRelayRefusesOtherActuationSurfaces` |
| B4 | Raw automation/script config create/update/delete (`/api/config/automation|script/config/*`) skips `plan write`/`plan delete`: a new automation can run at once; a delete has no typed check | Refused; the refusal names `ha-nova plan write` / `plan delete` | `TestRawRelayRefusesConfigPlanBypass` |
| B5 | Raw dashboard delete (`lovelace/dashboards/delete`) skips the typed `plan delete --domain dashboard` | Refused | same test |
| B6 | Changing what the tier engine reads: a cover's registry `device_class` override (garage → curtain) makes the next open ordinary | Raw `config/entity_registry/update` with `device_class` on a `cover.*` entity is refused (UI only) | `TestRawRelayRefusesCoverClassOverride` |
| B7 | `blueprint/save` overriding a blueprint changes automations that already use it | Raw `blueprint/save` with `allow_override` is refused; a new blueprint (no consumers yet) stays allowed | same test |
| B8 | Bus subscriptions expose `call_service` data (a future phone channel's secret) | `subscribe_events` without an `event_type`, with `*`, or `call_service`, and `subscribe_trigger` naming `call_service`, are refused, bare or inside `collect_events` | `TestRawRelayRefusesCallServiceSubscriptions` |
| B9 | Plan file at rest: `"code"` in plain text, and the code is a public function of the stored nonce, so a model reads or recomputes it, or forges a whole plan file | The file stores no code. The code is four digits of `HMAC-SHA256(Relay credential, "ha-nova/confirm-code/v1" ‖ identity ‖ channel ‖ expires_at)`; the credential never leaves the CLI. Plans move to `schema_version: 2`; older plans are refused (`PLAN_EXPIRED`, re-plan) | `TestPlanFileHoldsNoUsableCode` |
| B10 | Form plan applied with a code from the CLI, a `--no-forms` session, or after the form was declined | A plan made where the client shows forms is bound to the form (`channel: form`, inside the MAC). `apply` accepts a code for it only from the MCP server's own form answer (process memory, never argv); a CLI or no-forms apply is refused before recompute (`CONFIRM_IN_FORM`, no attempt used, the plan stays). Declining leaves it form-bound | `TestFormPlanRefusesTypedCode` |
| B11 | `homeassistant.restart` was preview: config written before (raw files) loads without a typed step, while every reload was typed | Restart is typed (`RELOAD_UNENUMERATED`) | tier golden row |

Editing the plan file (channel, expiry, tier, request) changes the MAC input or the recomputed hash, so the code no longer matches; minting a new plan file needs the credential. Apply also refuses a plan whose `expires_at` is more than its TTL ahead (`B12` below), so an editor cannot stretch the 4-digit code into an unbounded online brute force.

| B12 | Plan file edited with a far-future `expires_at` to keep guessing the 4-digit code past its 15-minute life | `apply` refuses a plan whose expiry is more than `planTTL` ahead | `TestApplyRefusesFarFutureExpiry` |
| B13 | Guard/Relay path-parse mismatch: `POST /api/services/lock/unlock#/../../../template` cleans to `/api/template` in the guard while the Relay drops the `#fragment` and forwards the unlock | The guard drops the `#fragment` and `?query` before normalizing, exactly as the Relay's URL parse does | `TestRawRelayRefusesFragmentPathTricks` |
| B14 | Guard fail-open on a JSON number the Relay's JS parser accepts but Go's float64 overflows (`"id":1e1000`), hiding a `call_service` from the guard | The guard decodes with `UseNumber`, so such a payload still parses and the WS type is seen | `TestRawRelayRefusesNumberOverflow` |
| B15 | Guard/Relay parse mismatch on whitespace and control characters: the Relay's URL parse strips a leading space or an inner tab/newline (and maps `\` to `/`), so ` /api/services/…` passed the guard; the read allowlist (`readablePath`, `ha_read`) had the same gap, e.g. `/api/states/.\t./webhook/x` | Both refuse any path holding whitespace, a control character, `\`, or `#` (read side) before matching | `TestRawRelayRefusesWhitespaceAndControlPaths` |

### 2.1 Raw writes are deny-by-default (follow-up to #716)

The denylist above could not know every integration's own API: `zwave_js/set_value`, a ZHA cluster write, or `POST /api/alarmo/…` still moved a lock or panel through `ha-nova relay ws|core` with no plan. Now (`cli/relay_raw_guard.go`, list in `cli/relay_raw_allow.go`):

- A raw core write (POST/DELETE) is sent only when method and exact path match an allowlist entry; the path must already be in the form the Relay forwards (no `%`-escapes, dot segments, doubled slashes, backslashes), so the path matched is the path Home Assistant routes. Core GETs stay open, except webhooks, backup downloads, and the Supervisor proxy `/api/hassio` (a backup holds every stored credential).
- A raw WS request is sent only when its exact `type` (HA dispatches on the exact string: no trimming or case folding) is on the allowlist — reads and writes alike, so an unknown type is refused whether it reads or writes. The read verbs' allowlist (`readOnlyWSTypes`) is a subset (test-enforced).
- The allowlist is exactly the raw writes the skills still make; each entry carries a one-line reason, and none acts on a device by itself: config-entry/options flows and entry delete, scene config, `check_config`, `/api/template`, helper/person/zone/tag and area/floor/label/category collections, entity/device registry updates (a cover's or button's class is refused: it sets the tier), orphan registry removal, dashboard metadata and resources (`/hacsfiles/` or `/local/` URLs only), energy prefs, three recorder statistics repairs, backups (not restore), HACS (not adding a custom repository), account create/delete, voice pipelines and exposure, calendar event writes, `todo/item/move`, a new blueprint.
- Removal or reconfiguration that would lower a later call's tier: a raw entry delete, options flow, or reconfigure flow for an entry that owns a lock, alarm panel, or access cover (a `switch_as_x` lock included), a raw registry removal of such an entity, and a rename of a button whose entity ID or name holds a destructive word (factory, wipe, erase), are refused after one registry read (`cli/relay_raw_targets.go`); disabling or enabling a lock, alarm panel, or cover raw is refused (a disabled one no longer marks its device's buttons and switches typed).
- No longer raw: `lovelace/config/save`, `lovelace/dashboards/create` (→ `plan write --domain dashboard`), `lovelace/config` with `force`, `supervisor/api`, custom-integration POSTs (→ the integration's UI panel), and every unlisted type.
- `/files` and `/backups` stay as before. A raw config-entry reload is refused (it ran `homeassistant.reload_config_entry` untyped; the skills call that service). A YAML file then takes effect through a typed reload or restart, or when a finished options or reconfigure flow reloads its entry (residual, §4).

The CLI's guided App update (`relay_guided_update.go`) sends one fixed `update.install` for the registry-proven NOVA Relay App after a yes on an interactive terminal. It is not a raw route (no model input reaches it), and `ha-nova call update.install --skill ha-nova:updates` tiers the same install preview, so it skips no tier.

A typed plan's channel is sealed (`channel_seal`, an HMAC under the Relay credential; plans move to `schema_version: 3`): a form plan edited to `chat` in the plan file is refused (`PLAN_INVALID`). Without it, argv codes would be accepted against a code nobody saw, and the mismatch counter in the same file would not stop guessing. With the seal the counter's home is moot: a chat plan's code is in `say`, a form plan never takes an argv code.

### 2.2 Services that used raw routes now run through `ha-nova call`

- `--response`: adds `?return_response` and returns `service_response` in `result`. The read-only response services (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`) get it implicitly and are no longer refused; they are tiered like any call (a matching `call_service` listener still raises them).
- `--skill <owner>`: an owned service (`mqtt.publish`, `notify.*`, `media_player.*`, `todo.*`, `calendar.*`, `camera.*`, `tts.*`, `recorder.purge*`, `update.*`, `backup.create*`, `logger.set_level`, `conversation.process`, `hassio.*`) is still refused (`OWNED_SERVICE`) unless the call names its owning skill. The engine tiers it with the existing member rules: `mqtt.publish`, `hassio.*`, and now `conversation.process` typed (each can move any device), other owned services preview.

## 3. Human channel precedence and the fallback (E)

- Channel at plan time: a confirmation window where one can open (`window`, `docs/work/2026-10-17-human-confirmation-channels-spec.md`); otherwise `form` when the plan is made inside `ha-nova mcp` for a client with form elicitation (not `--no-forms`); else `chat`.
- Detection is not model-controlled per plan: the channel is set by the MCP server from the client's handshake, carried in the MAC, and a `form` plan never falls back.
- `chat` plans keep the code in `say` and add one sentence: "Note: the assistant can see this code too, so it only counts if you type it yourself in your reply." Skills: the Safety Core already forbids typing the code; it now says the code is never typed, passed, or derived by the agent.

## 4. Residual risks (stated, not solved)

- **E is not human-only.** In skill mode the model sees the code. A model that ignores the Safety Core can plan with the CLI and pass the code itself; in a host with both a shell and the MCP server, that also routes around the form. Closing it needs a CLI-side human channel (native dialog, phone) — the next PR in the channels report's order.
- **T3 on every platform:** the model's user can reach the Relay credential — directly on Linux/Windows/containers/files, and on macOS by making `ha-nova` hand it over (redirect the Relay URL/pin in the user-writable config, or `pair --credential-store=file`). With it, the model derives any code or calls the Relay directly. The keychain ACL protects only against a *direct* reader, not against `ha-nova` itself. Only an HA-side gate (a credential the agent never holds), or binding the credential slot to the pinned endpoint plus a human prompt before the keychain→file migration, closes this. Neither is in this change.
- **Guard vs. Relay parse:** the CLI guard (Go) and the Relay (TypeScript) parse each request independently. B13/B14 closed the two known divergences; a Relay-side denylist (needs an App release) would remove the whole divergence class and is the stronger follow-up noted in §2's last row.
- **Mismatch counter** lives in the plan file: it limits slips, not editors. With a form plan there is no argv oracle (its channel is sealed, §2.1); a `chat` plan's code is visible anyway.
- **Indirect through allowlisted config:** a zone, person, calendar, or helper-config write can make an existing automation fire (presence, calendar, or state triggers), and that automation is not tiered by the raw write. Exposing an entity to a voice assistant lets a person's voice reach it. HACS installs from its store bring third-party code (frontend code runs at the next browser load; integration code after a typed restart); raw dashboard resources are limited to `/hacsfiles/` and `/local/`, and custom HACS repositories are added in the UI. Tiering these needs a plan verb with a consumer scan (follow-up).
- **Core GETs stay open:** a custom integration that acts on a GET is not stopped.
- **Flows reload their entry:** finishing an options or reconfigure flow reloads the entry; an integration that merges YAML on load (MQTT, KNX) then picks up YAML written through `/files`. Flows of entries that own a lock, alarm panel, or access cover are refused at their start; steps of a flow the CLI did not start (one the person opened) are not checked.
- **Indirect through stored items:** an edited scene or helper takes effect when an existing automation uses it; `ha-nova call` tiers activations it makes itself, not ones Home Assistant makes later. A raw edit of a cover's or button's class, and a raw rename of a destructive button, are refused; a device rename can still change a button's friendly name, which the `DESTRUCTIVE_BUTTON` heuristic reads.
- **Admin raw writes with skill-minted codes** (config-entry delete, scene/backup/to-do deletes, HACS removal) stay skill-enforced as before; a CLI plan for raw writes (`plan raw`) is follow-up work.
- **Updates:** `update.install` of Home Assistant itself restarts it and stays preview.
- A Relay-side denylist (stronger than the CLI guard against a model that calls the Relay directly) is out of scope: it needs an App release.

## 5. Changed behavior (for the PR body)

See §2 and §3; the PR body lists every refused route with its replacement. Also:
- A raw-route refusal exits 4 (the verbs' "refused"), not 1, and says it is a safety rule, never a setup or connection problem.
- `--set` keeps a number as the typed string when JSON would change it (`version=1.10` stays `"1.10"`, `1e3` stays `"1e3"`); objects, lists, booleans, and exact numbers are typed as before.
- `homeassistant.stop`, `recorder.purge`, `recorder.purge_entities` are typed.
