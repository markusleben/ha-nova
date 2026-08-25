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

- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova relay core --method POST --path /api/conversation/process --body-file <payload-file>` to test an utterance
- `--out <result-file>` for large listings

## Flow

1. **Testing an utterance** (the most useful thing here): `POST /api/conversation/process` with `{"text":"turn on the kitchen light","language":"en"}` (add `"agent_id"` to target a specific agent). The response says what Assist understood and what it did.
   - **This ACTUALLY EXECUTES the command.** "Turn on the light" turns the light on. Treat it as a service call: preview it, confirm it, and never fire a test utterance that changes something without asking.
   - A read-only utterance ("what is the temperature in the kitchen") is safe to run directly.
   - `"conversation_id"` continues a previous exchange; omit it for a fresh one.
2. **Pipelines**: WS `assist_pipeline/pipeline/list` shows every pipeline plus `preferred_pipeline`. Each carries `stt_engine`, `conversation_engine`, `tts_engine`, and language settings.
   - change the preferred one: WS `assist_pipeline/pipeline/set_preferred` with `pipeline_id`
   - create: WS `assist_pipeline/pipeline/create`. The named default is **clone-preferred-with-a-different-engine**: read the preferred pipeline from the list, carry its settings fields over, add the REQUIRED new `name`, and swap only the engine(s) the user asked for (the local-LLM move: same STT/TTS, different `conversation_engine`). Resolve the new engine id from step 4's inventories — never invent one. Preview every field, labeling cloned vs changed, confirm, then create; verify by re-reading the pipeline list for the new pipeline with the requested settings. Creating never changes the preferred pipeline — offer `set_preferred` separately
   - update: WS `assist_pipeline/pipeline/update` — read the pipeline first, then send ALL its settings fields with your change, addressed by `pipeline_id` (the list's `id` value). Never send it as `id` — that slot is the WS request id. A partial payload drops settings.
   - delete: WS `assist_pipeline/pipeline/delete` — typed confirmation code; a pipeline in use by a satellite breaks it
3. **Exposed entities** (what voice can even see): WS `homeassistant/expose_entity/list`; expose or hide with WS `homeassistant/expose_entity` (`assistants: ["conversation"]`, `entity_ids`, `should_expose`). Aliases live in the entity registry (`ha-nova:organize` owns those).
   - Exposing an entity gives voice control over it — preview the list before changing it.
   - Risk-weight the preview: exposing a `lock`, `alarm_control_panel`, or a cover with a garage/gate/door `device_class` means anyone within earshot can actuate physical access by voice — flag these entities explicitly as high-consequence before confirming.
4. **Engines**: WS `tts/engine/list`, `stt/engine/list`, `conversation/agent/list`, `wake_word/info`. Read-only inventories; use them to explain what a pipeline can be built from.
5. Verify pipeline changes by re-reading the pipeline list, and exposure changes by re-reading `expose_entity/list` — never report success from the command response alone. After a pipeline or exposure change made to FIX an utterance, offer to re-run that exact utterance as the proof (with the standing warning that a test utterance executes what it understands).

## Custom Sentences

Teach Assist a phrase it does not understand out of the box. This skill owns the workflow; the file paths, payload mechanics, and search query live in `skills/fallback/relay-ready.md` → Assist Custom Sentences — read that section first, do not duplicate it here.

1. **Opt-in gate**: sentence files need the relay's opt-in file access (`ha-nova:yaml-config` → Bootstrap explains enabling it). Without it, stop and say so.
2. **Write** the sentence file (and, only when the intent is new, the `intent_script:` block in `configuration.yaml`) per the fallback section's mechanics — `backup: true`, read the whole file before replacing it.
3. **Validate BEFORE any reload**: when `configuration.yaml` changed, `POST /api/config/core/check_config` FIRST. On `invalid`, restore that file's `.bak` immediately and do not reload.
4. **Reload the right thing**: `conversation.reload` reloads the sentence matcher — enough when only sentence files changed. A handler in an existing `intent_script:` block reloads via `intent_script.reload` — a new intent changed BOTH files, so run `conversation.reload` too; only the FIRST-ever top-level `intent_script:` block takes a Home Assistant restart (the integration is not yet set up). Say which applies; no phrase test before.
5. **Live test is mandatory**: run the exact phrase through `POST /api/conversation/process` (Flow step 1 rules — a test utterance executes what it matches). A sentence file that parses is not a sentence Assist matched; never claim success without this test.
6. **Rollback distinguishes reload from restart**: a failed phrase test after a sentence-file change → restore the `.bak` (or delete the new file), `conversation.reload` again, re-test one known-good phrase. A rolled-back `intent_script` drops its stale handler with `intent_script.reload`; if the block never loaded (first-block), the restore alone suffices.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Assist specifics:
- `assist_pipeline/run` is NOT usable here: it is a streaming subscription (audio), and the relay is request/response. Testing an utterance goes through `/api/conversation/process` instead.
- An unknown WS command usually means an older Home Assistant — say which version introduced it rather than guessing a workaround.
- A conversation response of "Sorry, I couldn't understand that" is a real answer, not an error: it means the intent did not match. Report what Assist actually said.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

Render the Report shape (output-rules.md). For an utterance test: the exact response text Assist gave, what it did (or did not) match, and which entities it touched. For pipelines: name, engines, language, and which one is preferred. Never paraphrase Assist's answer into something friendlier than it was.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- **A test utterance is a live command.** `conversation/process` executes what it understands. Anything that could change state gets a preview and confirmation, exactly like a service call.
- An utterance is the least enumerable indirect actuation there is: what it reaches is decided by the conversation agent at runtime. When the utterance plausibly reaches a lock, alarm panel, or access cover — the words themselves, or the exposed entity set makes it reachable — it takes the typed `confirm:<token>` (context skill → Confirmation Tiers), including the re-run proof after an exposure fix.
- Exposing entities to voice grants voice control over them — show the full list before changing exposure.
- Pipeline updates resend every settings field: read first, or you silently drop settings.
- No change here has a `revert`: restore exposure by re-toggling, restore a pipeline by resending its prior fields. A deleted pipeline recreates with a new `pipeline_id` — satellites pointing at the old one must be re-pointed.

## Guardrails

- One pipeline or one exposure change per operation.
- Never invent `pipeline_id` or `agent_id` values — list them first.
- Never claim Assist "understood" something the response does not show.
