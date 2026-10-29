# Confirmation texts in the household's language and in everyday words

Status: active

Date: 2026-10-28, updated 2026-10-29. Owner: CLI confirmation (`cli/confirm_*.go`, `cli/say_lines.go`, `cli/plan_raw*.go`, `cli/mcp_confirm.go`). Target: 0.28.0 (first candidate v0.28.0-rc5).

## Today

- Every confirmation text is English, built by HA NOVA from fixed sentences: `say` (chat), `plan.Prompt` (window, form), the phone ask (title, body, hint, button; `cli/confirm_phone_text.go`), outcomes (`cli/confirm_both.go`), the drift and demo wrappers (`cli/verb_apply.go`, `demoPrefix`), and the settings asks (link, move, stop phone confirmations in `cli/confirm_phone_link.go`; windows off in `cli/confirm_window.go`).
- In chat the agent translates `say`; phone, window, and form stay English. Nothing reads HA's language.
- Raw plans and raw service calls describe the request, not its effect: `mqtt.publish` on `zigbee2mqtt/bridge/request/device/options` reads as "sends a raw MQTT message" plus topic and payload. A user can't tell what changes in the home.

## Goal

1. Every confirmation surface speaks Home Assistant's language: phone, window (Mac, Windows, Linux), form, outcomes, settings asks, and the chat `say`. One rule everywhere; English and German in 0.28.0.
2. The first line says in everyday words what happens in the home; a second line keeps the technical detail for those who want it. Example (de): "Rollladen Links Wohnzimmer: Richtung umdrehen" / "Zigbee2MQTT-Einstellung invert_cover → true".
3. The texts stay HA NOVA's own: the agent never writes or translates them on phone, window, or form, and no MCP tool text changes (`TestMCPToolTextStable` green, "Always allow" in claude.ai survives).

Non-goals: setup page, NOVA page, pairing page, terminal lines (`phoneTapLine`, `phoneNothingHint`, …; the agent translates them), a per-user language, languages beyond en and de.

## Rejected

- The agent supplies or translates the text: a manipulated model could show "Turn on the light?" while the plan unlocks a door.
- Original plus translation: unreadable for who can't read the original; no room on a phone.
- A language argument from the agent: changes tool text and lets the chat pick the phone's language.
- A runtime AI translation in the home: not everywhere, slow, not repeatable, steerable by names.
- A separate "parts" data model rendered per surface (earlier draft): the builders already run again at apply from the stored plan, so one stored, sealed language is enough. Less code, same guarantee.

## Design

1. Language: at plan time HA NOVA reads `language` from `/api/config` (the REST twin of `get_config`) for every plan that asks something (preview and typed) and stores it as `plan.Lang`. `de-CH` → `de`; a language without a catalog, or an unreadable config, is `en`. Every text of that plan, at plan and at apply, uses `plan.Lang`: a typed plan's re-plan (window, phone, form, chat fallback, drift) is pinned to it. Settings asks without a plan read it live.
2. Seal: `channelSeal` (`cli/confirm_code.go`) adds `Lang` and encodes its fields as one JSON array, so no field can borrow another's bytes (seal v2, plan schema 4: an earlier version's plan is refused as expired). A typed plan's `Lang` must be a catalog this binary has, else the plan is refused (a newer binary's plan never crashes an older one). An edited language is refused like an edited prompt. A `config/core/update` that sets `language` becomes typed (`cli/plan_raw_tier.go`), as it changes every later confirmation.
3. Catalog: `cli/confirm_strings.en.json` (source) and `cli/confirm_strings.de.json`, same format and loader as `cli/setupweb/strings.en.json` / `cli/setup_strings.go` (keys, named placeholders, plurals; German uses the English plural rule). The builders keep their structure; each literal becomes a key, each concatenated fragment a whole sentence with placeholders, so word order belongs to the language. Names, ids, topics, and payload keys are inserted verbatim and quoted (`sayText`).
4. Words: every security-relevant word is ours: all access and disruptive actions (`lock`, `alarm_control_panel`, `cover`, `siren`, `button`, `homeassistant.restart`/`stop`, `lock.unlock`/`open` included), every `sayVerbs` entry, config verbs, reason clauses, raw lines, frames, hints, buttons, outcomes, wrappers, and the form field. Any other service uses HA's own name in the plan's language (`frontend/get_translations`, category `services`, `component.<domain>.services.<service>.name`), read once per plan and stored in the sealed plan.
5. Title frame per language: our verbs read naturally (en "Unlock Front Door?", de "Haustür entriegeln?"); an HA-supplied action name, whose case we can't adapt, uses `{names}: {action}?` (de "Wohnzimmer: Einschalten?").
6. Everyday line, then detail: each confirmation opens with what happens in the home ("Rollladen Links Wohnzimmer: Richtung umdrehen", "Gerät Flurlampe löschen, mit 3 Entitäten"). Where that differs from the request, a detail line follows ("Zigbee2MQTT-Einstellung invert_cover → true"). The phone shows the everyday line; window, form, and chat show both. Raw plans, raw MQTT, device delete, and Supervisor actions are in 0.28.0.
7. Recognizers never understate: an everyday line comes only from an exact match of request shape and known keys (Zigbee2MQTT bridge requests `device/options` with known option keys, `device/rename`, `device/remove`, `permit_join`). Anything else reads "Sends a message straight to a device or bridge; HA NOVA can't check what it does" plus the verbatim detail. The tier never depends on a recognizer.
8. Safety logic works on ids and numbers, never words: `phoneButton`'s dismiss check moves to action ids, `phoneTitle`'s cut uses the numeric count, the macOS answer matches the label passed for that ask.
9. Never mixed: an ask renders wholly in one language; a missing key or HA action name makes the whole ask English, and a test fails on any missing key so it can't ship. Known limit: Windows' stock OK/Cancel follow Windows' language.
10. Translation: German is written in the PR and reviewed by the maintainer; CI never translates.

## Delivery

Four stacked PRs, each reviewed and merged before the next builds on it; rc5 follows the last.

1. Foundation, no visible change: catalog loader, language read, stored and sealed language, typed language change, catalog window labels with the macOS answer matched to them.
2. German for typed and config plans on every surface, settings asks, chat `say`; HA's own action names; phone title and button decided on ids and counts (Design 8).
3. Everyday lines for raw plans, raw MQTT, device delete, and Supervisor actions (English first).
4. German for raw plans.

## Verification

- Golden tests per surface in en and de against a fake Relay whose `get_config` says `de` and whose translations hold one non-catalog service: phone payload and outcomes, window text and buttons, form, settings asks, chat `say`. English goldens byte-identical except the new detail lines; `TestMCPToolTextStable` unchanged.
- One plan, one text: phone body, window prompt, form message, and `say` carry the same everyday and detail lines.
- Seal: editing `Lang` in the plan file is refused; changing HA's language between plan and apply keeps the stored one.
- Catalog: every key in both files, matching placeholders and plurals, phone limits on the rendered German text (`phoneTitleLimit`, Android hint 38, pop-up titles ~30), no "token"/"PIN", every tier reason code and access action has text.
- Recognizers: each known Zigbee2MQTT request renders its line; an unknown topic, key, or extra field renders the generic line.
- Lanes: window on Windows via `npm run vm-lane`; phone on the HAOS VM with HA set to German; the maintainer reads a real iPhone and Android push in German.

## Known limits

- One language per household; a plan keeps its language if HA's changes before apply.
- Non-access action names are as good as HA's translations.
- Everyday lines exist for recognized requests only; the rest reads generic plus detail.
- Windows' window buttons follow Windows' language.
