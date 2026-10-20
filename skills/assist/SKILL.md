---
name: assist
description: Use when working with Home Assistant's own built-in voice assistant (Assist) — testing what it understands, inspecting, editing, or creating Assist pipelines, managing which entities are exposed to voice, listing TTS/STT/wake-word engines, and teaching Assist custom sentences — through HA NOVA Relay. Not for Alexa or Google Assistant — setting those up is `ha-nova:integration-setup` (or `ha-nova:yaml-config` for a manual cloud config), and one that STOPPED working is a concrete failure for `ha-nova:diagnose`.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Assist

## Scope

Home Assistant's built-in voice assistant (Assist):
- **test what Assist understands** — send an utterance and see the real answer, without speaking
- inspect, create, and edit Assist pipelines (which STT, conversation agent, TTS a pipeline uses) and change the preferred one
- teach Assist custom sentences (see Custom Sentences below)
- manage which entities are exposed to voice, and their aliases
- list available TTS, STT, conversation, and wake-word engines

Not in scope: speaking through a speaker (`ha-nova:media` for TTS announcements), microphone/satellite hardware setup, or the third-party assistants (Alexa/Google) — setup goes to `ha-nova:integration-setup` (or `ha-nova:yaml-config` for a manual cloud config), and a failure ("Alexa stopped working") to `ha-nova:diagnose` like any other integration incident.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

`ha-nova call` returns the preview: confirm it once, then `ha-nova apply --plan <id>`.

- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova call conversation.process --skill ha-nova:assist --response --set text="<utterance>"` to test an utterance (always typed: a sentence can control any device)
- `--out <result-file>` for large listings

## Flow

1. **Testing an utterance** (the most useful thing here): `ha-nova call conversation.process --skill ha-nova:assist --response --set text="turn on the kitchen light"` (`--set language=en`, `--set agent_id=<id>` optional). `result.service_response.response` says what Assist understood and did.
   - **This ACTUALLY EXECUTES the command.** Every test utterance is typed (the CLI can't tell a read-only phrase from an actuating one) — even a "what is the temperature" query.
   - `--set conversation_id=<id>` continues a previous exchange; omit it for a fresh one.
2. **Pipelines**: WS `assist_pipeline/pipeline/list` shows every pipeline plus `preferred_pipeline`. Each carries `stt_engine`, `conversation_engine`, `tts_engine`, and language settings.
   - change the preferred one: WS `assist_pipeline/pipeline/set_preferred` with `pipeline_id`
   - create: WS `assist_pipeline/pipeline/create`. The named default is **clone-preferred-with-a-different-engine**: read the preferred pipeline from the list, carry its settings fields over, add the REQUIRED new `name`, and swap only the engine(s) the user asked for (the local-LLM move: same STT/TTS, different `conversation_engine`). Resolve the new engine id from step 4's inventories — never invent one. Preview every field, labeling cloned vs changed, confirm, then create; verify by re-reading the pipeline list for the new pipeline with the requested settings. Creating never changes the preferred pipeline — offer `set_preferred` separately
   - update: WS `assist_pipeline/pipeline/update` — read the pipeline first, then send ALL its settings fields with your change, addressed by `pipeline_id` (the list's `id` value). Never send it as `id` — that slot is the WS request id. A partial payload drops settings.
   - delete: WS `assist_pipeline/pipeline/delete` — one-time code; a pipeline in use by a satellite breaks it
3. **Exposed entities** (what voice can even see): WS `homeassistant/expose_entity/list`; expose or hide with WS `homeassistant/expose_entity` (`assistants: ["conversation"]`, `entity_ids`, `should_expose`). Aliases live in the entity registry (`ha-nova:organize` owns those).
   - Exposing an entity gives voice control over it — preview the list before changing it.
   - Risk-weight the preview: exposing a `lock`, `alarm_control_panel`, or a cover with a garage/gate/door `device_class` means anyone within earshot can actuate physical access by voice — flag these entities explicitly as high-consequence before confirming.
4. **Engines**: WS `tts/engine/list`, `stt/engine/list`, `conversation/agent/list`, `wake_word/info`. Read-only inventories; use them to explain what a pipeline can be built from.
5. Verify pipeline changes by re-reading the pipeline list, and exposure changes by re-reading `expose_entity/list` — never report success from the command response alone. After a pipeline or exposure change made to FIX an utterance, offer to re-run that exact utterance as the proof (with the standing warning that a test utterance executes what it understands).

## Custom Sentences

Teach Assist a phrase it does not understand out of the box. This skill owns the workflow; the file paths, payload mechanics, and search query live in `../fallback/relay-ready.md` → Assist Custom Sentences — read that section first, do not duplicate it here.

1. **Opt-in gate**: sentence files need the relay's opt-in file access (`ha-nova:yaml-config` → Bootstrap explains enabling it). Without it, stop and say so.
2. **Write** the sentence file (and, only when the intent is new, the `intent_script:` block in `configuration.yaml`) per the fallback section's mechanics — `backup: true`, read the whole file before replacing it.
3. **Validate BEFORE any reload**: when `configuration.yaml` changed, `POST /api/config/core/check_config` FIRST. On `invalid`, restore that file's `.bak` immediately and do not reload.
4. **Reload the right thing**: `conversation.reload` reloads the sentence matcher — enough when only sentence files changed. A handler in an existing `intent_script:` block reloads via `intent_script.reload` — a new intent changed BOTH files, so run `conversation.reload` too; only the FIRST-ever top-level `intent_script:` block takes a Home Assistant restart (the integration is not yet set up). Say which applies; no phrase test before.
5. **Live test is mandatory**: run the exact phrase through `ha-nova call conversation.process` (Flow step 1 — a test utterance executes what it matches). A sentence file that parses is not a sentence Assist matched; never claim success without this test.
6. **Rollback distinguishes reload from restart**: a failed phrase test after a sentence-file change → restore the `.bak` (or delete the new file), `conversation.reload` again, re-test one known-good phrase. A rolled-back `intent_script` drops its stale handler with `intent_script.reload`; if the block never loaded (first-block), the restore alone suffices.

## Error Handling

Full relay/upstream error taxonomy: `../ha-nova/relay-api.md` -> Error Handling. Assist specifics:
- `assist_pipeline/run` is NOT usable here: it is a streaming subscription (audio), and the relay is request/response. Testing an utterance goes through `ha-nova call conversation.process` instead.
- An unknown WS command usually means an older Home Assistant — say which version introduced it rather than guessing a workaround.
- A conversation response of "Sorry, I couldn't understand that" is a real answer, not an error: it means the intent did not match. Report what Assist actually said.

## Output Format

Apply `../ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there (`../ha-nova/output-cards.md`).

Render the Report shape (output-cards.md). For an utterance test: the exact response text Assist gave, what it did (or did not) match, and which entities it touched. For pipelines: name, engines, language, and which one is preferred. Never paraphrase Assist's answer into something friendlier than it was.

## Safety

- Preview before write. The only exception is a call that `ha-nova call` itself executed as `tier: ordinary`; everything else waits for the user's confirmation of the shown preview. High-consequence actions (e.g. unlocking or opening a lock, opening a gate, garage, or entry door, disarming the alarm — also through a scene or script) and deletes or other destructive operations need the user's window click or phone tap, else their one-time code from HA NOVA; "yes" or any other reply is invalid.
- Confirmation binds to the displayed preview; any target, payload, endpoint, or scope change voids it (context skill → Active Preview Confirmation).
- Relay a plan's `say` in full, localized; never drop or shorten sentences.
- Anything before the preview ("do it", answers to questions) never confirms it; only a reply to the preview does.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through the `ha-nova` CLI.
- Any HA write this skill does not cover: STOP, invoke `ha-nova:fallback` first; never probe unfamiliar write endpoints.

- Drafts follow `../ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- **A test utterance is a live command.** `conversation/process` executes what it understands. Anything that could change state gets a preview and confirmation, exactly like a service call.
- An utterance is the least enumerable indirect actuation there is: what it reaches is decided by the conversation agent at runtime. When the utterance plausibly reaches a lock, alarm panel, or access cover — the words themselves, or the exposed entity set makes it reachable — it takes the one-time code (context skill → Confirmation Tiers), including the re-run proof after an exposure fix.
- Exposing entities to voice grants voice control over them — show the full list before changing exposure.
- Pipeline updates resend every settings field: read first, or you silently drop settings.
- No change here has a `revert`: restore exposure by re-toggling, restore a pipeline by resending its prior fields. A deleted pipeline recreates with a new `pipeline_id` — satellites pointing at the old one must be re-pointed.

## Guardrails

- One pipeline or one exposure change per operation.
- Never invent `pipeline_id` or `agent_id` values — list them first.
- Never claim Assist "understood" something the response does not show.
