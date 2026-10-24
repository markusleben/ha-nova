# Chat apps for more Home Assistant users (2026-10-24)

Status: active

Target: 0.28. Parents: `docs/work/2026-10-19-chat-bridge-contract.md` (the bridge), `docs/work/2026-10-18-onboarding-0.27-spec.md` §11.3–11.4. Needs the phone control channel from `docs/work/2026-10-24-chat-app-phone-move-spec.md` (land that first).

## Problem

- Chat apps are owner-only in practice. The bridge already reads a list of allowed Home Assistant users (`allowed_users` in `chat-apps.json`, `cli/mcp_serve.go:285-309`), but nothing writes it: the NOVA page only shows "Who can use it: only you" (`nova/src/http/handlers/nova-page-view.ts:255,268`), and the App action accepts no user list (`nova/src/http/handlers/chat-apps.ts:14`).
- The bridge acts through the App's own Relay access (`cli/mcp_serve_relay.go`), which can do everything an administrator can. So a user on the list gets admin power today, whatever their role in Home Assistant.
- Only the owner's phone can be linked for chat apps, and only from a computer's setup (`cli/chat_apps.go:216-238`). Another user on the list gets chat codes or nothing.
- Home Assistant's logbook shows the App, not the person, for every chat-app change. Nobody can tell who unlocked the door from a chat.

## Goal

- On the NOVA page, the owner picks which Home Assistant users may use HA NOVA in chat apps.
- The owner picks a level per user: **Ask only**, **Use the home**, or **Manage everything**. A level never gives more than Home Assistant gives that user: Manage everything is for administrators only.
- "Use everything, change nothing": Use the home controls every device and runs existing scripts and scenes, but never creates, edits, or deletes anything.
- Each user's confirmations go to that user's own linked phone, never to anyone else's.
- The owner sees who changed what, and removing a user works at once.

## Non-goals

- Per-device or per-area permissions (Home Assistant has none to mirror).
- Signing a user out of their chat app from the NOVA page (Home Assistant has no API to delete another user's sign-ins; see UX).
- A user linking their own phone without the owner (later: needs a new tool, which resets "Always allow"; see the directory spec).
- Logging reads.

## Design

### Who can be picked

- The page lists Home Assistant users from `config/auth/list` (already read for the owner check, `nova/src/security/owner-check.ts`): active, not system-generated, not the Relay's own user. Each row shows the name, "Owner", "Administrator", or "User", and the level picker.
- The owner is always allowed and can't be unticked. Only the owner ticked: the App writes no list (today's default, "the current owner", `cli/mcp_serve.go:291-298`), so an ownership change in Home Assistant still works. The demo keeps its own hand-written list (an explicit list stays the whole list).
- New App save action, written atomically next to the switches (`setSwitches`, `nova/src/runtime/chat-apps.ts:377-380`, today writes only `read_only` and `chat_codes`). It checks every id against a fresh `config/auth/list` first; an unknown, inactive, or system user refuses the whole save. It re-reads the file after that wait, so a switch changed meanwhile isn't overwritten.
- **Rollback guard:** whenever the App writes an explicit list, it also writes `"roles": 1`. Not only when a user below Manage everything is on it: an administrator can be demoted in Home Assistant later, with no save on the NOVA page to notice. A 0.27 bridge rejects unknown fields (`DisallowUnknownFields`, `cli/mcp_serve.go:329-333`), so after a rollback to the 0.27 App it refuses every chat request instead of giving such a user full power. The cost: after a rollback, chat apps stay refused until the owner updates the App again (the 0.27 page has no "only me" control, and turning chat apps off doesn't reset the list); the page and `nova/DOCS.md` say so.

### Levels (bridge, on every request)

The bridge already re-reads `chat-apps.json` per request. It now also reads the user's role from `config/auth/list` (the owner read at `cli/mcp_serve.go:373-391` becomes a read of all users, same cache). The role sets a cap: owner or `system-admin` → Manage everything; `system-users` → Use the home; anything else (`system-read-only`, no group, unknown) → Ask only.

- **Effective level = the lower of the stored level and the role's cap**, computed on every fresh read. A user with no stored level (an older list, the demo's hand-written one) gets the cap. The save refuses a level above the cap. A demoted administrator so falls to the highest level the new role allows, with no save on the NOVA page.
- Default when ticked: the cap.

| Level | Tools |
|---|---|
| Manage everything | all 10 tools, as today; typed plans: their own linked phone, else a chat code if the owner allows codes |
| Use the home | `ha_find`, `ha_get` (state only: no `config`, `related`, or `references`, which read automations, scripts, and dashboards, `cli/mcp_tools.go:56-62`), `ha_history`, `ha_skill`; `ha_call` within the allowlist below; `ha_apply` only for this user's own `kind: call` plans (the phone confirmation path, `chatApply`, `cli/mcp_serve_phone.go:157`); everything else refused |
| Ask only | the Use the home row without `ha_call` and `ha_apply` |

**The allowlist (fails closed).** The bridge acts with the App's admin access, so Home Assistant's own admin-only checks never run, and Home Assistant has no API that marks a service admin-only. A denylist would leave `homeassistant.restart`, `hassio.*`, `update.install`, `recorder.purge`, `backup.*`, `shell_command`, `rest_command`, and the `reload` services open. So Use the home allows only:

- Device domains: light, switch, fan, cover (all, garage and gate included), lock, alarm_control_panel, valve, siren, climate, media_player, vacuum, humidifier, water_heater, input_boolean, input_number, input_select, select, number, button, input_button.
- Running what exists: `scene.turn_on`, `script.turn_on` and calling a script entity, `automation.trigger`. Never `script`/`automation` `turn_off`, `toggle`, `reload`, or any create, edit, or delete path.
- `homeassistant.turn_on/turn_off/toggle` only when every resolved target is in the lists above.
- The check runs on the effective service and every resolved target after the tier engine resolves them (`cli/plan_call.go:282-301`). A `skill` argument is refused for Use the home (it unlocks owned services, `cli/plan_call.go:233-247`).
- High-consequence actions, also inside a scene or script, follow the tier engine as for an administrator: they need this user's own linked phone. **No chat code for Use the home:** the model can see a chat code. With no usable phone the answer is refused with "Ask <owner's name> to link your phone on the NOVA page."
- Allowed device changes still trigger automations, as in Home Assistant. Copy says "can't change automations", never "no automations".

- The tool list stays the same for everyone (no hash change). Refusals use the existing refusal envelope with a plain `next` (as `chatRefusal`, `cli/mcp_serve_phone.go:120-126`). `chatPhoneMissing` and `chatCodesOffNext` (`cli/mcp_serve_phone.go:44-46`) name "administrator" and `ha-nova setup confirm-phone`; both get per-level text.
- Level reads: the cached list (1 minute) for reads; a fresh read before any change (`ha_call`, `ha_apply`, a phone tap's apply, a chat code's apply). **At apply, the plan is re-checked against the user's current effective level** on both the phone path and the code path: its kind, and for a `call` plan the stored service, `skill`, and resolved targets against the allowlist again. So an administrator demoted between the ask and the tap can apply neither a config plan nor a call outside the allowlist (e.g. `homeassistant.restart`). An unreadable list refuses every request (503), as an unreadable owner does now.

### Phones per user

- Users at Use the home or Manage everything get a phone the same way as the owner: the owner starts it on the NOVA page (row → "Link phone"), the link question goes only to that user's own phones (their `person`'s Companion devices), and only a tap that arrives as that user links it. The phone spec's control channel runs the asks and writes `phone-links.json`.
- Chat-app phones use a new check, `phoneAllowed(user, level)`: the user is on the list at a level above Ask only. The computer path keeps `phoneAdmin` (`cli/confirm_phone_setup.go:281`, `cli/confirm_phone_link.go:345`) unchanged.
- A user without a `person`, or a phone not assigned to their person, has no phones to pick; the row says "Add Anna's phone to Anna under Settings → People." If the phone's Companion app is signed in as another user, its tap arrives as that user and is refused; the row warns before linking.
- The bridge already sends a typed plan's ask only to the requesting user's link, and refuses a tap from anyone else (`discoverConfirmer`, `cli/mcp_serve_phone.go:73-79`; `linksForCall`, `:85-105`). This stays as is.

### Activity

- The bridge appends one line per change to `bridge/activity.jsonl` (owned by the bridge, `0600`): time, the user's name at that moment, the tool, the service or plan kind, target names, and the result (done, refused, confirmed by phone, confirmed by code, outcome unknown). No reads, no chat text, no codes.
- Capped at 500 lines or 30 days, whichever is first; trimmed on write.
- The NOVA page shows the last 20 under "Recent changes from chat apps".

### Removing a user

- Untick → saved at once; the next request from that user gets the existing 403. A phone ask already open is re-checked before it applies (`cli/mcp_serve_phone.go:241-256`, unchanged).
- The App then removes that user's phone link with the control channel's `drop` action, which the bridge accepts only for a user no longer in `allowed_users` (phone spec). The App writes the list first, then drops. If the drop fails, nothing is lost: the bridge ignores links of users not on the list, and re-adding the user asks for a new link.
- Their Home Assistant sign-in in the chat app stays until they or the owner remove it in Home Assistant; it can no longer do anything here.

## Security

- **Never wider than Home Assistant.** Below Manage everything, nobody reaches configuration, raw reads, or plans, and `ha_call` stays inside the allowlist, whatever the model asks. The check is in the bridge, on the user id the integration vouched for (secret plus `X-HA-User-Id`), never on anything the model sends.
- **No confirming for someone else.** Plans and phone asks stay bound to the user who made them. A user's ask goes only to their own link; a link's tap counts only from that same user while their effective level allows the action. The owner's phone never gets another user's ask, and another user's tap never confirms the owner's plan.
- **Known limits (admins remain admins):** an administrator who knows another phone's webhook id can forge a tap (`docs/work/2026-10-17-human-confirmation-channels-spec.md` §7.7, T3), and one with file access can read the `/mcp` secret from Home Assistant's config entry and call the bridge as any allowed user, so activity lines can be faked by administrators. Both need Home Assistant admin power, which already unlocks doors directly.
- **No widening by a non-owner.** Only the owner's ingress session (`checkOwner`) writes `allowed_users`; the bridge never writes `chat-apps.json`; no tool reads or changes it. An administrator who adds a new user in Home Assistant doesn't add them here (the list holds ids).
- **Admins remain admins.** An allowed administrator can already change users and roles in Home Assistant itself; HA NOVA adds nothing to that.
- **Fail closed:** an unreadable settings file or user list refuses; a missing or unknown group caps at Ask only; a 0.27 bridge after a rollback refuses (rollback guard).
- **Demo:** unchanged. Its one user is an administrator and its list is written by its reset script.

## UX copy (draft)

NOVA page, Chat apps section:

> **Who can use it**
> Pick who may use HA NOVA in Claude and ChatGPT. Each person signs in with their own Home Assistant account.
> ☑ Markus — Owner · Manage everything
> ☐ Anna — [Use the home ▾] · [Link phone]
> ☐ Ben — [Ask only ▾]
> [Save]
>
> **Ask only:** questions only.
> **Use the home:** all devices, scenes, and scripts. Includes doors, gates, and the alarm; their phone asks first. Can't change automations or settings.
> **Manage everything:** what you can do. Only for Home Assistant administrators.

Row status, from the bridge's last request per user: "Not signed in yet" / "Signed in · no phone" / "Phone: Anna's iPhone · [Change] [Stop]".

Before saving a user with no own login (the family shares one account): "Anna needs her own Home Assistant login. Add one under Settings → People."

Under each ticked user, "How Anna joins" (folded):
> 1. In Claude or ChatGPT, add HA NOVA with this link: [copy]
> 2. Sign in with your own Home Assistant login.
> 3. Ask Markus to link your phone here.

Removing someone: "Ben can't use HA NOVA in chat apps anymore. To sign Ben's chat app out of Home Assistant too, Ben removes it under Profile → Security."

Chat, a user refused a change:

> I can't change that for you. Someone who manages Home Assistant can.

Chat, Use the home with no linked phone:

> This needs a tap on your phone, and none is linked yet. Ask Markus to link your phone on the NOVA page.

Recent changes:

> **Recent changes from chat apps**
> 18:02 · Anna · Unlocked Front Door · confirmed on her phone
> 17:40 · Anna · Turned off Kitchen Lights

## Test plan

- Go (`cli/mcp_serve_test.go`, fake Home Assistant): a role × level table (owner, admin, `system-users`, `system-read-only`, no group, no stored level, level above cap, demoted between ask and tap on the phone and the code path (a pending config plan and a pending `homeassistant.restart` call plan both refused), unreadable list) × each tool; Use the home: `ha_call` outside the allowlist refused before anything is sent (`homeassistant.restart`, `hassio.*`, `update.install`, `script.turn_off`, `homeassistant.turn_on` on a mixed target set, any `skill`); `script.turn_on` and a lock with a linked phone go through the phone; a high-consequence call with no phone refuses without a code; `ha_apply` on a non-call plan refused; `ha_get` with `config`, `related`, or `references` refused; every explicit list carries `"roles": 1`, and it makes a 0.27-shaped decoder refuse.
- Go: user B's plan id from user A answers as unknown (exists; keep); B's ask never reaches A's phone; a tap from A on B's ask is refused.
- Go: `activity.jsonl` gets one line per change, none per read, holds no code, and trims at the cap.
- TS (`tests/http/`): the save refuses unknown, inactive, and system ids; a non-owner admin gets 403; the owner can't be unticked; removing a user drops their phone link.
- Page render test: hostile user and device names (activity list) render inert. Page copy that says only the owner can use chat apps ("Only you can use it", "sign in … as its owner", `nova/src/http/handlers/nova-page-view.ts`, chat section) is updated.
- Test bed E2E: two users signed in through the integration's view (the pytest harness), one admin and one user; the Use the home user turns a light off, unlocks with a tap on the simulated phone, and is refused `homeassistant.restart`.

## Decisions (maintainer, 2026-10-24)

1. Three levels per user instead of a fixed role mapping (Design → Levels). Use the home includes locks, gates, garage doors, and alarms.
2. Use the home confirms high-consequence actions on the user's own linked phone.
3. Use the home runs existing scripts and scenes but never changes them ("use everything, change nothing").
4. Activity log: keep.
5. The owner always stays on the list.
6. Rollback guard: accepted.

Agent defaults after the 2026-10-24 review (security, UX, feasibility): allowlist, not denylist; no chat code for Use the home; the role caps the level; level names Ask only / Use the home / Manage everything.
