# Confirmation texts in the household's language

Status: active

Date: 2026-10-28. Owner: CLI confirmation (`cli/confirm_*.go`, `cli/mcp_confirm.go`). Target: the release after 0.28.0.

## Today

- Chat works already: the CLI builds `say` in English and the agent relays it in the user's language (`cli/mcp_plain.go` `chatCodeRule`, `cli/mcp_server.go` `mcpResultRule`, `skills/ha-nova/output-rules.md` → Localization).
- The trusted surfaces are English only:
  - phone push (`cli/confirm_phone_text.go`: title, body, hint, button; verbs from `sayVerbs` in `cli/confirm_say_text.go`, reason phrases from `sayPhrase` in `cli/confirm_say.go`);
  - computer window (`plan.Prompt` plus `windowAsked`/`windowBoth` in `cli/confirm_window.go`; macOS buttons `Cancel`/`Confirm` hard-coded in `cli/confirm_window_darwin.go`, same on Linux and Windows);
  - app form (`cli/mcp_confirm.go` `confirmForm`: `plan.Prompt` as message, field title "One-time code from HA NOVA").
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

1. Language source: the plan reads `language` from `get_config` (it already talks to HA at plan time) and stores it in the plan. Phone, window, form, and the apply recheck render in that stored language. Region subtags fold (`de-CH` → `de`); a language without a catalog is English. Nothing the model sends picks it. A guest asking in English still gets the household language on the linked phone, by intent.
2. Render from parts, not English sentences. A confirmation becomes data: the goal (action plus names), the reasons (the tier reason code plus its variant, e.g. `ACCESS_LOCK`/unlock, `ACCESS_COVER`/garage, `DISRUPTIVE`/restart, already enumerated in `cli/tier_rules.go`), flags (no undo, copy saved, count of other items), and who asked (computer name or "your AI chat app"). One renderer per surface turns this into text from the language's catalog. Names stay exactly as written, as today.
3. Words:
   - Our catalog holds every security-relevant word: all `sayVerbs` verbs, the config verbs (Create, Delete, Restore, Undo the last change to), every reason phrase (locks, alarm, cover classes, destructive buttons, disruptive actions), frames, hints, buttons, and the form field. Access words never come from a third party.
   - Any other action (today's generic `sayVerb` fallback, e.g. `vacuum.start` → "Start") uses Home Assistant's own action name in the plan's language: `frontend/get_translations`, category `services`, key `component.<domain>.services.<service>.name`. Read once per plan, like states.
   - The title frame is per language in the catalog, so word order fits (en `{action} {names}?`, de `{names} {action}?`); a language whose grammar does not fit uses the neutral `{action}: {names}?`.
4. Catalog: `cli/confirm_strings.en.json` is the source; `confirm_strings.<lang>.json` per translation. Same format and loader as `cli/setupweb/strings.en.json` / `cli/setup_strings.go` (keys, named placeholders, CLDR plurals); the plural rule grows per language there (German uses the English rule).
5. Never mixed: an ask renders wholly in one language. Any missing key, missing HA action name, raw plan (phase 1), or unknown language makes the whole ask English.
6. Maintenance by script, review by people: `npm run strings:translate` fills missing keys from English with an LLM CLI and writes the files; the maintainer reviews the diff in the PR. CI never translates (deterministic builds). Tests fail on a missing key, a placeholder mismatch, a missing plural form, phone limits (`phoneTitleLimit`, the Android hint at 38 characters, pop-up titles at about 30), forbidden words ("token", "PIN"), and a tier reason code without a phrase.

## Surfaces

- Phone: title, button, hint, short reason, "can't be undone", requester line. An overlong reason is cut as today ("more in the chat").
- Window: prompt, `windowAsked`/`windowBoth`, buttons on every OS.
- Form: message, field title and description.
- Unchanged: the chat `say` and every terminal line (`phoneTapLine`, `phoneNothingHint`, …) stay English; the agent translates them in chat. English goldens stay byte-identical.

## Phases

1. English and German for typed and config plans: data model, catalogs, renderer, language read, tests.
2. Raw plans: `cli/plan_raw_say.go` goals and reasons and the `cli/relay_raw_allow.go` route descriptions move into the catalog.
3. More languages through the script, each with one review in its PR.

## Known limits

- One language per household. A plan keeps its language if HA's language changes before apply.
- Non-access action names are as good as Home Assistant's translations; access actions always use our words.
- Phase 1: raw plans (device deletion, Supervisor actions, …) stay English on every surface.

## Verification

- Go golden tests per surface and language against a fake Relay whose `get_config` says `de` and whose translations include one non-catalog action: phone payload, window text and buttons, form. English goldens unchanged; `TestMCPToolTextStable` unchanged.
- Catalog tests as in Design 6. New files under about 400 lines; split the catalog loader from the renderer.
- Lanes: window on Windows via `npm run vm-lane`; phone and form on the HAOS VM chat-app lanes with HA set to German. The maintainer reads one real iPhone and one real Android push in German (lock-screen and pop-up cut-offs only show there).

## Open questions

- Per-language title order versus the neutral `{action}: {names}?` everywhere: decide with the German strings in phase 1.
- The window follows HA's language, not the computer's, so one rule holds everywhere. Revisit only if users ask.
