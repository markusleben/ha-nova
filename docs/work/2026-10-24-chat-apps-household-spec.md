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
- Each user gets what Home Assistant would give them, never more: an administrator works as the owner does; a non-administrator can ask and control everyday devices only.
- Each user's confirmations go to that user's own linked phone (or their chat code), never to anyone else's.
- The owner sees who changed what, and removing a user works at once.

## Non-goals

- Per-device or per-area permissions (Home Assistant has none to mirror).
- Signing a user out of their chat app from the NOVA page (Home Assistant has no API to delete another user's sign-ins; see UX).
- A user linking their own phone without the owner (later: needs a new tool, which resets "Always allow"; see the directory spec).
- Phones for non-administrators (they never get typed plans, so they need none).
- Logging reads.

## Design

### Who can be picked

- The page lists Home Assistant users from `config/auth/list` (already read for the owner check, `nova/src/security/owner-check.ts`): active, not system-generated, not the Relay's own user. Each row shows the name and "Owner", "Administrator", or "User".
- The owner is always allowed and can't be unticked. Only the owner ticked: the App writes no list (today's default, "the current owner", `cli/mcp_serve.go:291-298`), so an ownership change in Home Assistant still works. The demo keeps its own hand-written list (an explicit list stays the whole list).
- New App save action, written atomically next to the switches (`setSwitches`, `nova/src/runtime/chat-apps.ts:377-380`, today writes only `read_only` and `chat_codes`). It checks every id against a fresh `config/auth/list` first; an unknown, inactive, or system user refuses the whole save. It re-reads the file after that wait, so a switch changed meanwhile isn't overwritten.
- **Rollback guard:** whenever the list holds anyone who isn't an administrator, the App also writes `"roles": 1`. A 0.27 bridge rejects unknown fields (`DisallowUnknownFields`, `cli/mcp_serve.go:329-333`), so after a rollback to the 0.27 App it refuses every chat request instead of giving a User full power. The cost: chat apps stay refused until the owner removes those users or updates again; the page and `nova/DOCS.md` say so.

### What each role may do (bridge, on every request)

The bridge already re-reads `chat-apps.json` per request. It now also reads the user's role from `config/auth/list` (the owner read at `cli/mcp_serve.go:373-391` becomes a read of all users, same cache). Role from Home Assistant's groups (`haUser.GroupIDs`, `cli/confirm_phone_setup.go:86-99`): owner or `system-admin` → Administrator; `system-users` → User; anything else (`system-read-only`, no group, unknown) → Read-only.

| Role | Tools |
|---|---|
| Owner, administrator | all 10 tools, as today; typed plans: their own linked phone, else a chat code if the owner allows codes |
| User | `ha_find`, `ha_get` (state only: no `config`, `related`, or `references`, which read automations, scripts, and dashboards, `cli/mcp_tools.go:56-62`), `ha_history`, `ha_skill`; `ha_call` only for the ordinary tier and only in the everyday device domains (light, switch, fan, cover except access covers, climate, media_player, vacuum, humidifier, water_heater, scene, input_boolean, input_number, input_select, select, number); everything else refused |
| Read-only | the User row without `ha_call` |

- Why that list: Home Assistant lets a non-administrator call services and read states, but not read or change configuration (automations, integrations, traces, logs). A fixed domain list is simpler than mirroring Home Assistant's admin-only services one by one, and it fails closed.
- A User's `ha_call` that needs a preview or a code is refused, not planned ("ask someone who manages Home Assistant"). `ha_plan`, `ha_apply`, `ha_read`, `ha_map`, and `ha_trace` are refused for Users and Read-only users.
- The tool list stays the same for everyone (no hash change). Refusals use the existing refusal envelope with a plain `next` (as `chatRefusal`, `cli/mcp_serve_phone.go:120-126`).
- Role reads: the cached list (1 minute) for reads; a fresh read before any change (`ha_call`, `ha_apply`, a phone tap's apply). A user demoted meanwhile is refused. An unreadable list refuses every request (503), as an unreadable owner does now.

### Phones per user

- Users who are administrators get a phone the same way as the owner: the owner starts it on the NOVA page (row → "Link phone"), the link question goes only to that user's own phones (their `person`'s Companion devices), and only a tap that arrives as that user links it. The phone spec's control channel runs the asks and writes `phone-links.json`.
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

- **Never wider than Home Assistant.** A non-administrator can't reach configuration, raw reads, typed actions, or plans, whatever the model asks. The check is in the bridge, on the user id the integration vouched for (secret plus `X-HA-User-Id`), never on anything the model sends.
- **No confirming for someone else.** Plans and phone asks stay bound to the user who made them. A user's ask goes only to their own link; a link's tap counts only from that same user while they are an administrator. The owner's phone never gets another user's ask, and another user's tap never confirms the owner's plan.
- **Known limits (admins remain admins):** an administrator who knows another phone's webhook id can forge a tap (`docs/work/2026-10-17-human-confirmation-channels-spec.md` §7.7, T3), and one with file access can read the `/mcp` secret from Home Assistant's config entry and call the bridge as any allowed user, so activity lines can be faked by administrators. Both need Home Assistant admin power, which already unlocks doors directly.
- **No widening by a non-owner.** Only the owner's ingress session (`checkOwner`) writes `allowed_users`; the bridge never writes `chat-apps.json`; no tool reads or changes it. An administrator who adds a new user in Home Assistant doesn't add them here (the list holds ids).
- **Admins remain admins.** An allowed administrator can already change users and roles in Home Assistant itself; HA NOVA adds nothing to that.
- **Fail closed:** an unreadable settings file or user list refuses; a missing or unknown group reads as Read-only, never as User or Administrator; a 0.27 bridge after a rollback refuses (rollback guard).
- **Demo:** unchanged. Its one user is an administrator and its list is written by its reset script.

## UX copy (draft)

NOVA page, Chat apps section:

> **Who can use it**
> Pick who may use HA NOVA in Claude and ChatGPT. Each person signs in with their own Home Assistant account.
> ☑ Markus — Owner
> ☐ Anna — Administrator · [Link phone]
> ☐ Ben — User
> ☐ Carla — Read only
> [Save]
>
> Administrators can do what you can. Their phone asks them before an unlock or a delete.
> Users can ask questions and switch everyday things, like lights. They can't unlock, open the garage, or change automations.
> Read-only users can only ask questions.

Under each administrator with a phone: "Phone: Anna's iPhone · [Change] [Stop]".

Removing someone: "Ben can't use HA NOVA in chat apps anymore. To sign Ben's chat app out of Home Assistant too, Ben removes it under Profile → Security."

Chat, a user refused a change:

> I can't do that for you. Someone who manages Home Assistant can, or you can do it in the Home Assistant app.

Recent changes:

> **Recent changes from chat apps**
> 18:02 · Anna · Unlocked Front Door · confirmed on her phone
> 17:40 · Ben · Turned off Kitchen Lights

## Test plan

- Go (`cli/mcp_serve_test.go`, fake Home Assistant): a role table (owner, admin, `system-users`, `system-read-only`, no group, demoted between plan and apply, unreadable list) × each tool; a User's `ha_call` outside the domain list, or with a preview or code tier, is refused before anything is sent; `ha_get` with `config`, `related`, or `references` refused for a User; `"roles": 1` makes a 0.27-shaped decoder refuse.
- Go: user B's plan id from user A answers as unknown (exists; keep); B's ask never reaches A's phone; a tap from A on B's ask is refused.
- Go: `activity.jsonl` gets one line per change, none per read, holds no code, and trims at the cap.
- TS (`tests/http/`): the save refuses unknown, inactive, and system ids; a non-owner admin gets 403; the owner can't be unticked; removing a user drops their phone link.
- Page render test: hostile user and device names (activity list) render inert. Page copy that says only the owner can use chat apps ("Only you can use it", "sign in … as its owner", `nova/src/http/handlers/nova-page-view.ts`, chat section) is updated.
- Test bed E2E: two users signed in through the integration's view (the pytest harness), one admin and one user; the user turns a light off and is refused an unlock.

## Open questions for the maintainer

1. Non-administrators: the fixed domain list above, or read-only for them in 0.28?
2. Should a non-administrator ever get typed actions confirmed on their own phone (Home Assistant lets them unlock in the app)? Default: no.
3. Activity log: keep it (default), or skip it for 0.28?
4. Must the owner always stay on the list (default), or may the owner leave it?
5. Rollback guard: accept that a rollback to the 0.27 App refuses chat apps while a User is on the list (default), or drop Users from the list on rollback some other way?
