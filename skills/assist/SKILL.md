---
name: assist
description: Use when working with Home Assistant's voice assistant — testing what Assist understands, inspecting or editing Assist pipelines, managing which entities are exposed to voice, and listing TTS/STT/wake-word engines — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Assist

## Scope

Home Assistant's built-in voice assistant (Assist):
- **test what Assist understands** — send an utterance and see the real answer, without speaking
- inspect Assist pipelines (which STT, conversation agent, TTS a pipeline uses) and change the preferred one
- manage which entities are exposed to voice, and their aliases
- list available TTS, STT, conversation, and wake-word engines

Not in scope: speaking through a speaker (`ha-nova:media` for TTS announcements), microphone/satellite hardware setup, or the third-party assistants (Alexa/Google) — those are configured outside Home Assistant.

## Bootstrap (once per session)

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
   - update: WS `assist_pipeline/pipeline/update` — read the pipeline first, then send ALL its settings fields with your change, addressed by `pipeline_id` (the list's `id` value). Never send it as `id` — that slot is the WS request id. A partial payload drops settings.
   - delete: WS `assist_pipeline/pipeline/delete` — tokenized confirmation; a pipeline in use by a satellite breaks it
3. **Exposed entities** (what voice can even see): WS `homeassistant/expose_entity/list`; expose or hide with WS `homeassistant/expose_entity` (`assistants: ["conversation"]`, `entity_ids`, `should_expose`). Aliases live in the entity registry (`ha-nova:organize` owns those).
   - Exposing an entity gives voice control over it — preview the list before changing it.
4. **Engines**: WS `tts/engine/list`, `stt/engine/list`, `conversation/agent/list`, `wake_word/info`. Read-only inventories; use them to explain what a pipeline can be built from.
5. Verify pipeline changes by re-reading the list — never report success from the command response alone.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Assist specifics:
- `assist_pipeline/run` is NOT usable here: it is a streaming subscription (audio), and the relay is request/response. Testing an utterance goes through `/api/conversation/process` instead.
- An unknown WS command usually means an older Home Assistant — say which version introduced it rather than guessing a workaround.
- A conversation response of "Sorry, I couldn't understand that" is a real answer, not an error: it means the intent did not match. Report what Assist actually said.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Render the Report shape (output-rules.md). For an utterance test: the exact response text Assist gave, what it did (or did not) match, and which entities it touched. For pipelines: name, engines, language, and which one is preferred. Never paraphrase Assist's answer into something friendlier than it was.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- **A test utterance is a live command.** `conversation/process` executes what it understands. Anything that could change state gets a preview and confirmation, exactly like a service call.
- Exposing entities to voice grants voice control over them — show the full list before changing exposure.
- Pipeline updates resend every settings field: read first, or you silently drop settings.
- No change here has a `revert`: restore exposure by re-toggling, restore a pipeline by resending its prior fields. A deleted pipeline recreates with a new `pipeline_id` — satellites pointing at the old one must be re-pointed.

## Guardrails

- One pipeline or one exposure change per operation.
- Never invent `pipeline_id` or `agent_id` values — list them first.
- Never claim Assist "understood" something the response does not show.
