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
5. Title frame per language: our verbs read naturally (en "Unlock Front Door?", de "Haustür entriegeln?"); an HA-supplied action name, whose case we can't adapt, is quoted as a name, in a frame that also completes the chat sentence "Ich werde gleich …" (de "“Temperatur einstellen” für “Wohnzimmer” ausführen?").
6. Everyday line, then detail: each confirmation opens with what happens in the home ("Rollladen Links Wohnzimmer: Richtung umdrehen", "Gerät Flurlampe löschen, mit 3 Entitäten"). Where that differs from the request, a detail line follows ("Zigbee2MQTT-Einstellung invert_cover → true"). The phone shows the everyday line; window, form, and chat show both. Raw plans, raw MQTT, device delete, and Supervisor actions are in 0.28.0.
7. Recognizers never understate: an everyday line comes only from an exact match of request shape and known keys (Zigbee2MQTT bridge requests `device/options` with known option keys, `device/rename`, `device/remove`, `permit_join`). Anything else reads "Sends a message straight to a device or bridge; HA NOVA can't check what it does" plus the verbatim detail. The tier never depends on a recognizer.
8. Safety logic works on ids and numbers, never words: `phoneButton`'s dismiss check moves to action ids, `phoneTitle`'s cut uses the numeric count, the macOS answer matches the label passed for that ask.
9. Never mixed: an ask renders wholly in one language; a missing key or HA action name makes the whole ask English, and a test fails on any missing key so it can't ship. Known limit: Windows' stock OK/Cancel follow Windows' language.
10. Translation: German is written in the PR and reviewed by the maintainer; CI never translates.

## Decisions

Calls of the coordinating agent, 2026-10-29, for Delivery 2b:

1. Raw plans are sealed as `en` until Delivery 4, so no raw ask mixes German sentences with English item lines. Delivery 4 also ships in 0.28.0 (maintainer: raw actions all in 0.28.0), so the pin lasts only between PRs; Delivery 4 ends it.
2. The link hint (`phoneUnlinkedNext`) quotes the link ask's title and button in the same live language as the settings ask it refers to, never in the plan's language.
3. State values (`locked → unlocked`) use HA's own names in the plan's language (`frontend/get_translations`, category `state`), read with the action names and stored in the sealed plan. They never replace a security word; a value without a translation stays English, like a name. Attribute and field names stay as they are.
4. The German batch order line reads „… und hört bei der ersten Änderung auf, die fehlschlägt.“, not „… die nicht erledigt wird.“

Built in 2b: Home Assistant keeps state names under category `entity_component` (`component.<domain>.entity_component._.state.<state>`), which 2b reads for the domains a call acts on. `unknown` and `unavailable`, which Home Assistant names only in its frontend, are HA NOVA's own words, like the drift line's word for a state; a value a call sends is shown as it is. An action's name counts only when both reads succeed, it differs from the English one, has a letter once defanged, and fits 40 runes (else English with a notice); it is never a phone button (Design 8: no word list can check it in every language). The setup page sends its link and move asks in Home Assistant's language, read with the phones, and quotes their buttons from the catalog in that language (the control state carries them; the page only renders them); its own copy stays English. Terminal lines quote the button of the ask they refer to the same way.

Calls of the maintainer, 2026-10-29, on the German texts:

1. Quotes: German texts mark names „…“, English ones “…”. `sayText` stays the one place a name is quoted and defanged. It turns every quote mark of either language („ “ ‚ ‘ included) into an apostrophe, so no name can close or open a quote, and the German catalog re-marks only the pairs HA NOVA wrote (`confirmQuote`). A phone's name in the link question is defanged the same way.
2. An integration's own action on an access entity (a Z-Wave „Benutzercode löschen“ on a lock) keeps Home Assistant's German name for the action; the tier and its warnings stay HA NOVA's.
3. The chat-app titles stay „Chat-App-Freigaben erhalten/wechseln/beenden?“.
4. Undo reads „rückgängig machen“, not „zurücksetzen“: „Die letzte Änderung an „Flurlicht“ rückgängig machen?“. After „an“ the German undo names the item without its kind, so the phone's question and result titles keep the name, cut by the title rule and never dropped.
5. Batch lines say „sind erledigt“ instead of „haben geklappt“, without a doubled word („Erledigt: alle {count}.“, „Teilweise erledigt: {done} von {count}; …“, „Unklar: {name} ist vielleicht erledigt; …“). „Am kritischsten:“ stays.
6. Delivery 4: raw lines keep the verb last, with details in parentheses before it. A blueprint is „Blueprint“, as Home Assistant's German frontend writes it, with its article and case („den Blueprint“, „einen Blueprint“), not „Blaupause“.
7. German names Home Assistant's concepts with Home Assistant's own word, as its German frontend writes it (`home-assistant-frontend` 20261028.1), with its article and case, as for „Blueprint“: „Automation“ and „Automationen“, not „Automatisierung“. A word Home Assistant uses in more than one sense, or one that would replace a security word, stays as it is. Applied by the coordinating agent to every concept noun in the German catalog: „Backup“, not „Sicherung“; helpers by their type name („den Zahlenwert-Eingabe-Helfer“, „Text-Eingabe-Helfer“, „Dropdown-Menü-Helfer“, „Zeitpunkt-Eingabe-Helfer“); „das Reparaturproblem“; „den Home-Assistant-Benutzer“, not „Konto“; „Datenauswertung“; „die Labs-Vorschaufunktion“; „die entdeckte Integration“; and exposing as „verfügbar machen“ (Home Assistant's toggle „Neue Entitäten verfügbar machen“) and „die Sprachassistenten für …“, so „Freigabe“ stays the chat-app word (item 3). „Räume“ stays, as the English says rooms too.

Call of the coordinating agent, 2026-10-29, for Delivery 4: ignore and hide read like dismissing, so they join Cancel, Close, Stop, and Dismiss in the one dismiss rule (Design 8), and a raw step's or label's ignore and hide button reads Confirm (Bestätigen). These buttons are Delivery 4's only intended English change: discovered-integration ignore, repair-issue ignore, and HACS update hide read Confirm instead of Ignore and Hide.

## Delivery

Four stacked PRs, each reviewed and merged before the next builds on it; rc5 follows the last.

1. Foundation, no visible change: catalog loader, language read, stored and sealed language, typed language change, catalog window labels with the macOS answer matched to them.
2. German for typed and config plans on every surface, settings asks, chat `say`; HA's own action names; phone title and button decided on ids and counts (Design 8). Two PRs: 2a moves every confirmation text into `cli/confirm_strings.en.json` (English byte-identical), threads the plan's language through every builder, and decides title and button on ids and counts; settings asks stay English. 2b adds `cli/confirm_strings.de.json`, HA's action names, and the live language for settings asks.
3. Everyday lines for raw plans, raw MQTT, device delete, and Supervisor actions (English first).
4. German for raw plans.

### Delivery 3: line shapes

Scope: `mqtt.publish` (raw MQTT), the five `hassio.*` calls `ha-nova call` runs (Supervisor actions), raw plans (their frame and leading copies), raw device delete, and raw Supervisor items. Every new or changed line is an English catalog key, written as a whole sentence with placeholders. Call plans speak the plan's language, so their keys carry German from the start: Delivery 2b's catalog owner wrote it after 2b landed, and it is copied unchanged (one agent's call below); raw plans stay sealed `en` (Decision 1), and Delivery 4 adds their German. The other raw item lines (`rawWSPhrases`, `rawItemLine`, `rawCoreLine`) move to the catalog with their German in Delivery 4.

1. One goal, every surface. The everyday line is the plan's `actionGoal`: a new verb id per recognized request, with its catalog phrase and its own button (Design 8). `say`, the window, and the form open with it (`I'm about to {goal}.`); the phone asks it as its title (`{goal}?`, cut on the names' count). What completes the goal (the settings a change sets, or what follows from it) comes after a colon in `say`, window, and form, and opens the phone body as a statement, so the title keeps its names within 80 characters. A title keeps at least 8 runes for its name, quotes included: with less, the object's kind goes first ("Remove “Flurlampe” …"), then the name is cut, never dropped; a result's title ("Not confirmed: …", „Hat nicht geklappt: …“) follows the same rule. The detail follows as its own sentence in `say`, window, and form (`Detail: {detail}.`), never on the phone, with one exception (agent's call in review): a generic line ("Send an MQTT message", "Run a Supervisor action") opens its phone body with its detail, each value cut at 60 characters with its length, since nothing else on the phone names its target, so a door-unlock message and a harmless one never read the same. A tier reason keeps its sentence. A direct MQTT call with its everyday line reads "This goes straight to a device or bridge, which HA NOVA can't check" (`owned.mqtt_line`), so it doesn't repeat the line; everywhere else (script and automation steps, a call with targets) `owned.mqtt` reads "sends an MQTT message straight to a device or bridge", so MQTT stays named. A recognized host restart or shutdown reads "This runs a Supervisor action, which HA NOVA can't check", since its line already says what happens. A call with targets gets no everyday line (neither service takes one) and reads as today, its targets named.
2. MQTT recognizers (Design 7). Only the default base topic `zigbee2mqtt/bridge/request/<request>`, service data exactly `topic` and `payload` (`qos` 0, 1, or 2 allowed; `retain` and `evaluate_payload` absent or false), and `payload` a string holding one JSON object with exactly these keys (Zigbee2MQTT docs, 2026-10). A name shows whole (up to 60 characters); a longer one reads generic.

   | Request | Payload | Everyday line (en) | After the colon, and first in the phone body | Button |
   |---|---|---|---|---|
   | `device/options` | `id`, `options` with only `invert_cover` (bool) and `occupancy_timeout` (whole seconds 1–86400; 0 turns the timer off and reads generic) | Change the Zigbee device “{id}” | "open and closed are swapped" (false: "open and closed are no longer swapped"), "it reports no motion {n} seconds after the last motion" | Change |
   | `device/rename` | `from`, `to`, optional `homeassistant_rename` (bool) | Rename the Zigbee device “{from}” | "its new name is “{to}”" (true: ", and its entity IDs in Home Assistant change too, so automations and dashboards that use the old ones need updating") | Rename |
   | `device/remove` | `id`, optional `force`, `block` (bool) | Remove the Zigbee device “{id}” from the network (force: "from Zigbee2MQTT only"; block: "… and block it") | block: "it can't rejoin until it is unblocked in Zigbee2MQTT"; force: "until it is factory reset, it keeps the network key and can still talk on the network"; both: "Zigbee2MQTT turns it away if it joins again, but until it is factory reset, …" | Remove |
   | `permit_join` | `time` (whole seconds 1–254), optional `device` | Let new devices join the Zigbee network for {n} seconds (device: "Let new devices join Zigbee through “{device}” for {n} seconds", so a 60-character name keeps its room behind "Not confirmed:") | | Allow |
   | `permit_join` | `time` 0, no `device` | Turn joining off, so no new devices can join the Zigbee network | | Confirm |

   Detail: `Zigbee2MQTT request “{request}” ({key} → {value}, …), MQTT topic “{topic}”`, nested keys by their path (`options.invert_cover`). Anything else (another topic or base topic, an unknown request or key, a wrong value type, a payload that isn't such a string, a bare-string payload, the 1.x `{"value": true}` form, retained or evaluated, another qos) reads "Send an MQTT message" with the detail `MQTT topic “{topic}”, payload “{payload}”` and every other field ("kept on the broker (retained)" when retained); button Confirm.
3. Verbatim detail: every value on a detail line (topic, payload, fields, a raw Supervisor path) shows up to 300 characters (today 60, `sayText`); a longer one ends in "…" and states its length. Quoting and defanging stay `sayText`'s.
4. Supervisor calls, exact data only: `addon_start`/`addon_stop`/`addon_restart` with exactly `{addon}` (an App slug, not `self`) read "Start / Stop / Restart the App “{name}” ({slug})", the name that of the App's device in Home Assistant (`config/device_registry/list`, identifier `["hassio", slug]`, a read every plan may make, over Home Assistant Link too; a call plan never reads the Supervisor store); no such device: the slug alone. `host_reboot` and `host_shutdown` without data read "Restart / Shut down the machine Home Assistant runs on"; a shutdown adds "Home Assistant stays off until someone turns the machine on again". Detail: `Supervisor action “hassio.{service}”`. Any other data: "Run a Supervisor action" with the detail `Supervisor action “hassio.{service}” ({key} → {value}, …)` and the App or host reason as today. Buttons: Start, Confirm (stop reads like dismissing), Restart, Restart, Shut down.
5. Raw plans open with the home. Leading `backups` saves (the config snapshot) are not steps in the opening, the count, the phone title, or the apply result's step numbers and counts; the text adds "HA NOVA saves a copy first ({names})." (after the step list's opening when there are several steps), and an apply with `--no-snapshot` swaps it for the no-copy note. A device delete with its copy asks "Delete the device “Flurlampe” and its 2 entities?" instead of "Confirm 2 steps in Home Assistant?". The frame sentences (the step list's opening, "Step {n}: …", "And {n} other steps", "Confirm {n} steps in Home Assistant") become catalog keys.
6. Device delete keeps its line as a catalog key ("Delete the device “{name}” and its {n} entities", with child devices as today) and gets the detail `device ID “{id}”`. Raw Supervisor items keep their lines (install, uninstall, automatic updates, settings) as catalog keys, the App named from the Supervisor store as today, with the detail `Supervisor {method} “{path}”`; what an uninstall deletes and the settings a change sets come after the colon (point 1). Other raw items get no detail in this delivery.
7. Tests: every recognizer row and every generic trigger above; the same line in `say`, window prompt, form, and phone title; buttons by id; titles within 80 characters; two different generic requests give different phone bodies; `TestMCPToolTextStable` unchanged.

German force-and-block remove (agent's call): „{object} nur in Zigbee2MQTT entfernen und sperren“ keeps the verb „entfernen“ of the button and every other remove line, and keeps a usual 16-character name whole behind „Hat nicht geklappt:“ at exactly 79 runes.

Known limits: a custom Zigbee2MQTT base topic and every other bridge read generic; the option table covers two keys; some sensors round `occupancy_timeout` up to their own detection interval.

### Delivery 4: German for raw plans

Scope: every text a raw plan's confirmation shows (raw item lines, their typed reasons, device delete and raw Supervisor lines, counts, dates, the step frame) speaks the plan's language, so Decision 1 ends.

1. Language: a raw plan that asks takes `plan.Lang` like every plan (`planLanguage` no longer pins it to English; raw plans read no Home Assistant names). `computeRawPlan` checks and tiers every item first, seals the plan, and only then builds the lines in `textLang(plan)`, the language the seal holds, as call plans build their everyday line. Reads that the hash covers (the device and entity registries a device delete removes, the energy lists) still happen before the seal; a raw plan that asks nothing stays English.
2. Lines: each raw line is a verb id and its object, both catalog keys (`raw.verb.*`, `raw.object.*`, `raw.ws.*`, with `raw.noun.*`, `raw.field.*`, `raw.event.*`, the month names, and the frames that join them). The frame `raw.goal` reads `{action} {object}` in English (byte-identical) and `{object} {action}` in German: the verb comes last, and what a change sets or entails stands in parentheses before it („die Entität „Desk“ (Name „Schreibtisch“) ändern“). The phone title cuts the object, never the verb, and a list of targets keeps its count. The button is the verb's own catalog word while it is one or two words; `cancel`, `ignore`, and `hide` (they read like dismissing, Decisions) and longer verbs get Confirm. Both are decided on the id and the catalog, never on Home Assistant's words (Design 8).
3. Typed reasons: the sentences of `plan_raw_tier.go` and the App reasons become `raw.why.*` keys. `reasons[].detail` keeps the English sentence, so the plan's JSON and hash stay as they are; the ask renders the matching key in the plan's language, and a sentence without a key reads as the unknown reason, never as harmless.
4. Counts and names per language (Delivery 3's note): device delete counts, App labels and settings, calendar dates, energy entries, and imported rows are built in the plan's language.
5. Stays English: refusals and errors for the agent, a raw apply's result (as every apply but a batch's), field names and values, ids.
6. Tests: a German home's raw plan speaks German end to end; every raw verb keeps its words on the phone in each catalog behind every result prefix; every typed reason has a key. A one-off golden of every raw line on every surface (chat, window, phone title, button, body, result titles), recorded on `main` before the change, is byte-identical in English after it, except the ignore and hide buttons (Decisions).

Known limits: English keeps its exact text, quirks included ("1 entries", "area no area", "1 days", the buttons Turn, Show, Read, Make). German gives each detail its own parentheses before the verb, and a cut title can leave one open. Behind a long result prefix, a request that names many targets on a type that takes one can cut the first name down to its quote marks.

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
