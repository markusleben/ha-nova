# Confirmation texts in the household's language

Status: active

Date: 2026-10-28. Owner: CLI confirmation (`cli/confirm_*.go`, `cli/mcp_confirm.go`). Target: the release after 0.28.0.

## Today

- Chat works already: the CLI builds `say` in English and the agent relays it in the user's language (`cli/mcp_plain.go` `chatCodeRule`, `cli/mcp_server.go` `mcpResultRule`, `skills/ha-nova/output-rules.md` → Localization).
- The trusted surfaces are English only:
  - phone push (`cli/confirm_phone_text.go`: title, body, hint, button; verbs from `sayVerbs` and the generic `sayVerb` fallback in `cli/confirm_say_text.go`, reason phrases from `sayPhrase` in `cli/confirm_say.go`), plus its outcomes (`cli/confirm_both.go`, e.g. "Approved on the computer");
  - computer window (`plan.Prompt` plus `windowAsked`/`windowBoth` in `cli/confirm_window.go`). macOS buttons `Cancel`/`Confirm` are fixed, and `darwinAnswer` matches the literal `button Confirm` (`cli/confirm_window_darwin.go`); Windows uses the stock OK/Cancel of `MessageBoxTimeoutW`, which follow Windows' own language (`cli/confirm_window_windows.go`);
  - app form (`cli/mcp_confirm.go` `confirmForm`: `plan.Prompt` as message, field title "One-time code from HA NOVA"). Only a local `ha-nova mcp` client gets forms; the chat bridge runs with `noForms` (`cli/mcp_serve.go`) and falls back to the chat code;
  - English wrappers around `plan.Prompt`: the drift prefix (`cli/verb_apply.go`) and `demoPrefix` (`cli/confirm_say.go`);
  - settings asks outside any plan: link, move, and stop phone confirmations (`phoneLinkAsk`, `phoneChangeAsk`, `cli/confirm_phone_link.go`) and turning windows off (`cli/confirm_window.go`).
- No surface reads a language: neither HA's `language` from `get_config` nor anything in the request.
- Result: a user who writes German reads German in chat and English on the phone, in the window, and in the form.

## Goal

Phone, window, and form speak the household's language. The text stays built by HA NOVA alone, and no MCP tool text changes, so `TestMCPToolTextStable` stays green and "Always allow" in claude.ai survives.

Non-goals: setup page, NOVA page, pairing page, terminal output, the chat `say` (the agent keeps translating it), raw plans in phase 1, a per-user language.

## Rejected

- The agent supplies the translation: a manipulated model could show "Turn on the light?" while the plan unlocks a door. These surfaces exist because the model can't write them.
- The agent's translation next to the English original: a reader who can't read English can't check the original, and the phone has no room for both.
- A language argument from the agent: changes tool text (resets "Always allow") and lets the chat, not the household, pick the phone's language.
- The household's own AI translating at runtime: not every home has one, it is slow and not repeatable, and names could steer it.
- Localization keys in the Companion app: they cover only the app's own strings.

## Design

1. Language source: the plan reads `language` from `get_config` (it already talks to HA at plan time) and stores it in the plan. Phone, window, form, outcomes, and the apply recheck render in that stored language. Settings asks without a plan read it live when they ask. Region subtags fold (`de-CH` → `de`); a language without a catalog, or an unreadable `get_config`, is English.
2. The model can't pick it: a `config/core/update` that sets `language` becomes typed, like time zone and units today (`cli/plan_raw_tier.go`), since it changes what every later confirmation says. Today it is preview-tier through `cli/relay_raw_allow.go`.
3. The stored language and every rendering input are sealed: `channelSeal` (`cli/confirm_code.go`) covers id, channel, kind, and `Prompt` today; it adds the language and the parts below. Tests edit the plan file's language and parts and expect a refusal, and change HA's language between plan and apply and expect the stored one.
4. Render from parts, not English sentences. A confirmation becomes data with everything the text uses today (`sayWhat`, `sayAsk`, `sayPhrase`): the goal (action id plus names), values and deltas (`0% → 40%`), area, warnings, reasons (tier reason code plus variant, member action, data, state, and `Via`, e.g. `ACCESS_LOCK`/unlock, `ACCESS_COVER`/garage, `DISRUPTIVE`/restart, enumerated in `cli/tier_rules.go`), flags (no undo, copy saved), the numeric count of other items, and who asked. One renderer per surface turns this into text from the language's catalog. Names stay exactly as written.
5. Safety logic works on ids and numbers, never on rendered words: `phoneButton`'s dismiss check (Cancel, Close, Stop, Dismiss) moves to action ids, and `phoneTitle`'s truncation uses the numeric count instead of parsing "and N more". The macOS answer is matched against the label HA NOVA passed for that ask, not a fixed English string.
6. Words:
   - Our catalog holds every security-relevant word: every action of the access and disruptive domains (`lock`, `alarm_control_panel`, `cover`, `siren`, `button`, `homeassistant.restart`/`stop`), including those that today reach `sayVerb`'s generic fallback (`lock.unlock`, `lock.open`), all `sayVerbs`, the config verbs (Create, Delete, Restore, Undo the last change to), every reason phrase, frames, hints, buttons, outcomes, wrappers, and the form field. Access words never come from a third party.
   - Any other action uses Home Assistant's own action name in the plan's language: `frontend/get_translations`, category `services`, key `component.<domain>.services.<service>.name` (allowlisted and passed by the Relay WS filter today). Read once per plan, like states, and sealed with it.
   - The title frame is per language in the catalog, so word order fits (en `{action} {names}?`, de `{names} {action}?`); a language whose grammar does not fit uses the neutral `{action}: {names}?`.
7. Catalog: `cli/confirm_strings.en.json` is the source; `confirm_strings.<lang>.json` per translation. Same format and loader as `cli/setupweb/strings.en.json` / `cli/setup_strings.go` (keys, named placeholders, CLDR plurals); the plural rule grows per language there (German uses the English rule).
8. Never mixed: an ask renders wholly in one language. Any missing key, missing HA action name, raw plan (phase 1), or unknown language makes the whole ask English. Exception, as a known limit: Windows' stock buttons follow Windows' language.
9. Maintenance by script, review by people: `npm run strings:translate` fills missing keys from English with an LLM CLI and writes the files; the maintainer reviews the diff in the PR. CI never translates (deterministic builds). Tests fail on a missing key, a placeholder mismatch, a missing plural form, phone limits on the final rendered text (`phoneTitleLimit`, the Android hint at 38 characters, pop-up titles at about 30), forbidden words ("token", "PIN"), and a tier reason code or access-domain action without catalog text.

## Surfaces

- Phone: title, button, hint, short reason, "can't be undone", requester line, outcomes. An overlong reason is cut as today ("more in the chat").
- Window: prompt with its drift and demo wrappers, `windowAsked`/`windowBoth`, macOS buttons. Windows keeps its stock buttons.
- Form (local `ha-nova mcp` clients only): message, field title and description.
- Settings asks: link, move, stop, and windows off, in the live HA language.
- Unchanged: the chat `say`, the chat-code flow, and every terminal line (`phoneTapLine`, `phoneNothingHint`, …) stay English; the agent translates them in chat. English goldens stay byte-identical.

## Phases

1. English and German for typed and config plans and the settings asks: data model, catalogs, renderer, language read, the typed language change, the seal, tests.
2. Raw plans: `cli/plan_raw_say.go` goals and reasons and the `cli/relay_raw_allow.go` route descriptions move into the catalog.
3. More languages through the script, each with one review in its PR.

## Known limits

- One language per household. A plan keeps its language if HA's language changes before apply.
- Non-access action names are as good as Home Assistant's translations; access and disruptive actions always use our words.
- Windows' window buttons follow Windows' language.
- Phase 1: raw plans (device deletion, Supervisor actions, …) stay English on every surface.

## Verification

- Go golden tests per surface and language against a fake Relay whose `get_config` says `de` and whose translations include one non-catalog action: phone payload and outcomes, window text and buttons, form, settings asks. Long German lists (truncation keeps the true count), a German dismissing verb (still "Confirm"), and the macOS answer with a German label. Seal tests from Design 3. English goldens unchanged; `TestMCPToolTextStable` unchanged.
- Catalog tests as in Design 9. New files under about 400 lines; split the catalog loader from the renderer.
- Lanes: window on Windows via `npm run vm-lane`; phone on the HAOS VM with HA set to German; the form through a local elicitation-capable MCP client on `ha-nova mcp` with phone and window unavailable (the chat bridge never shows forms). The maintainer reads one real iPhone and one real Android push in German (lock-screen and pop-up cut-offs only show there).

## Open questions

- Per-language title order versus the neutral `{action}: {names}?` everywhere: decide with the German strings in phase 1.
- The window follows HA's language, not the computer's, so one rule holds everywhere. Revisit only if users ask.
