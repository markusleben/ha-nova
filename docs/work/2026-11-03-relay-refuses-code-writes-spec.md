# Relay refuses code-tier writes; skills send them through a plan

Status: active

Date: 2026-11-03. Owner: CLI raw route (`cli/relay_raw_endpoint.go` `relayTyped`, `cli/plan_raw_tier.go`, `cli/plan_raw_person.go`) and the skills that write raw. Target: 0.28.2 (held back from 0.28.1). Shipped as two PRs, each safe alone: the skills first, then the CLI.

## Today

`ha-nova relay ws|core|files|backups` sends a write at once. A raw plan (`ha-nova plan raw`) rates the same request, and for a delete, a removal, a new account, a HACS download, a dashboard resource, an emptying energy save, a new id, area, or label on a lock, alarm panel, or door, or a person's trackers it asks the user's own confirmation: the window, the linked phone, or a code the CLI mints and checks. Many skills sent these writes through `relay` and gated them with four digits the agent minted and checked itself. Nothing but the skill text stood between the AI and the write.

Maintainer decision (2026-11-03): `relay` refuses them, every skill goes through a plan and its confirmation, person trackers included.

## Design

1. **Skills.** Every write a plan types goes as a `ha-nova plan raw --file <requests.json>` item, then `ha-nova apply`; the plan's `say` asks. A skill mints no code for these. A batch manifest's one code is the ask of one raw plan of at most 20 items. A device takeover rename the Access Check types carries `"typed":true`. Organize sends entity, device, and area updates as plan items, so the CLI rates the target's family.
2. **Relay refusal (`relayTyped`).** After the shared guard and the to-do move rule, relay tiers what it would send with the plan's own function (`rawItemTier`): the body, and a `collect_events` envelope's `message`; a core method upper-cased as the Relay sends it (and trimmed, so a padded method fails closed). No second list. The first pass reads nothing: every read the tier needs (`rawNamer.read`) records the miss and counts as failed, and a failed read counts as typed. A typed result that came only from such a read opens a session and tiers again live; a read that fails there still refuses. A typed write is refused with exit 4 and nothing is sent:

   `refused raw <what> (a safety rule, not a setup or connection problem): <why>, so only the user may confirm it, and only a plan asks them: put [{"endpoint":"<endpoint>","payload":<this request>}] in a file, run ha-nova plan raw --file <file>, show the user its say, and follow its next_step; nothing was sent`

   `<why>` is the plan's English reason (`rawWhys`), first letter lowercased; for a person update without `device_trackers` it adds "(an update without device_trackers empties them: to keep them, resend the list person/list shows)". `<this request>` is the exact `{"method":…,"path":…}` for a core request without a body, and `<the message itself, without a collect_events envelope>` for an envelope's message. A session that cannot open is an error (exit 1), nothing sent. `ha_read` and chat plans are unchanged: `ha_read` sends no writes, and `ha_plan` kind raw already tiers.
3. **Person trackers (`personFollows`, every route).** Home Assistant's `person/update` defaults a missing `device_trackers` to `[]` and replaces the stored list. A create that names trackers is typed. An update is typed when the tracker set it leaves differs from `person/list` (ids lowercased, order ignored), when the person is unknown or listed without `device_trackers`, the read fails, or the value is not a list of ids. Before, any update carrying the key was typed (a rename that resends the same trackers asked for a code) and one without it was a preview (it emptied the trackers on a natural yes).
4. **Tracker renames (`trackerFollowed`, every route).** Home Assistant drops a tracker from a person only when it is removed (`person`'s entity registry filter). A rename leaves the person following an id that the next tracker given it takes over, and a disabled tracker stops counting for their presence. So a `config/entity_registry/update` that renames or disables a device tracker a person follows (storage or YAML), or renames one onto such an id, is typed. A failed `person/list` read, or one without both lists (`storage`, `config`) or with a person lacking `device_trackers`, counts as followed.

## Tradeoffs

- Computing the tier keeps previewed writes (renames, settings, creates) on `relay`. Refusing every write would move every skill write into a plan: more churn for the same protection.
- Fail closed costs a refusal when Home Assistant can't be read; the plan then rates it typed and asks.
- Chat apps get the person rule only with the App, which bundles the CLI bridge: a same-tracker rename goes from typed to preview; a create with trackers, or an update without the key for a person with trackers, goes from preview to typed. This PR bumps no App: the 0.28.2 release prep bumps it once for every bridge change (maintainer decision, 2026-11-05).

## Known limits

- Zone and home-location changes that move presence stay previewed.
- A skill's `"typed":true` raises only its plan: the Access Check's consumer rule (a takeover rename onto an id an unlock automation uses, on a device with no lock) is skill text on `relay`.
- Relay reads, closes that session, then sends; a change in between is not checked again (a plan re-tiers at apply).
- A cross-family cleanup's raw requests share one plan and its ask; an automation or script delete in it keeps its own plan and ask.
- Rolling back a just-created YAML file that fails `check_config` now needs the user's confirmation (`delete_file` is typed).

## Proof

`cli/relay_typed_test.go`: every allowlisted write, relay refuses exactly what a plan types (also in an envelope); a static delete is refused with no request; a lock move and a calendar update that drops dates read live, a failed read sends nothing; the person and tracker rules on plan and relay. `tests/skills/*` pin the skill routes.
