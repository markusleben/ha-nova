# The NOVA page for Home Assistant administrators

Status: active (built for 0.31, Markus D5 "Ja, in 0.31")

Date: 2026-11-12. Owner: the Relay's NOVA page (`nova/src/http/handlers/nova-page.ts`, `nova-page-view.ts`, `nova/src/security/owner-check.ts`). High-risk: authorization of the App's management page. Source: HANO-37 J2 friction (2), plan 0.31 #9.

## Problem

Only the Home Assistant owner (the first account) may open the NOVA page (`checkOwner`, `nova/DOCS.md`). Other administrators see NOVA in the sidebar (`panel_admin`) but get "Only the owner can open NOVA". In a household where someone else set up Home Assistant, an administrator can't make the six-digit code for their own computer and is stuck until the owner opens the page.

## Change

1. **Who opens the page:** an active, non-system Home Assistant administrator: `is_active === true` (a positive read), `system_generated !== true`, and `is_owner === true` or `group_ids` (an array) containing `system-admin`. Read fresh from `config/auth/list` on every GET and POST, as today. A failed read answers 503; a user missing from the list, inactive, system-generated, or without a readable admin group is refused (403) and nothing else on the page is read or shown. Identity stays the Supervisor ingress peer plus its single `x-remote-user-id` header (`resolveIngressIdentity`), unchanged. `checkOwner(request, deps, { admins: true })` admits administrators and says whether the user is the owner (`isOwner`); without the option it stays owner-only for the other routes. `isAdmin` is the one predicate: `isApprover` (the approval view) uses it too, so the page and the view can't drift, and a `group_ids` that isn't a list no longer counts there either (it did through a string's `includes`). The owner check itself now needs `is_active === true` (it took a missing `is_active` as active). The chat-app role cap (`roleCap`) reads `group_ids` only as a list too.
2. **Permission per endpoint and action** (the page's only routes: `GET /`, `GET /home`, `POST /action`, `POST /home/action`, the icon):

   | What | Who |
   | --- | --- |
   | Open the page; see connected computers, the open code, the App's version, "Waiting for your OK" | administrators |
| The icon | anyone with an ingress identity (static, unchanged) |
   | `generate_code`, `cancel_code` (connect a computer) | administrators |
   | `revoke_device` (any computer) | administrators |
   | `reset_registry` (the broken-list recovery) | owner |
   | Every chat-app action (`chat_apps_on`/`off`, `chat_read_only`, `chat_codes`, `chat_users`, the chat-app phone actions) | owner |
   | `/chat-apps/v1/state` (setup's `chat-apps on`), arming pairing v2 (setup's owner sign-in; its other steps check the armed secret) | owner (unchanged) |
   | Approve view (`/approve`) | administrators (unchanged, `checkApprover`) |

   The action handler checks the role before it consumes the CSRF token: an action not on the administrators' list needs the owner, so a new action is owner-only until someone lists it (fail closed). An administrator's refused owner action answers 403 with the owner's name, and changes nothing. `revoke_device` refuses the App's own connections (chat bridge, safety check) for everyone: revoking the bridge would turn chat apps off around the owner (the confirm screen never offers them, so no token was mintable; this guards the action itself).
3. **What an administrator sees:** the same page without the chat apps section, chat activity, and the reset confirm; in their place one line: "Chat apps and their settings: only the owner, <name>, can change them." (only on an App with chat apps). The footer says who is signed in. Chat-app files are not read for them (no household list, phones, or activity). A non-admin's refusal no longer links the approval view: the view refuses them too.

Why administrators may connect and revoke computers: a paired computer reaches Home Assistant through the App with the App's own rights, which an administrator already has in Home Assistant itself (they can also stop or remove the App). Chat apps stay with the owner (plan 0.31 #9): they change who else in the household can act, and at which level.

## Review record

- KISS (Sonnet): one option on `checkOwner` instead of a second function, one action set, one `isOwner` flag in the view, a no-read branch for chat apps; guard the bridge device. Applied. It suggested dropping the bed script (the bed has no Supervisor); kept, because the Captain asked for a bed proof with real users and it checks Home Assistant's real `config/auth/list` shapes.
- Astra (gpt-6-astra, medium): **partly agrees** with the split (no Home Assistant escalation beyond an administrator's own rights; a paired computer outlives its maker's role, documented below). Applied: strict `is_active` for the owner too, `isApprover` with the same list check, the activity outside the chat section hidden too, the bridge device guard, the pairing-v2 and icon rows.

## Not changed

- Setup's sign-in pairing (pairing v2) still needs the owner; an administrator who signs in gets the six-digit code path, which they can now serve themselves on the NOVA page. Opening pairing v2 to administrators is a separate change.
- The Relay's other listeners and the gate.

## Accepted limits

- Any administrator can revoke any computer, the owner's included. Home Assistant already lets them remove the App.
- A computer an administrator connected keeps its access after that person loses the role; revoke it on the page (as with the owner's computers today).
- `page.link` (the approval link on the old refusal) stays in the worker's text catalog unused; dropping it touches every language file and the CLI.
- Two administrators share one open code at a time (the pairing manager holds one); the second "Connect a device" replaces the first code, as two clicks by the owner do today. The same holds for an owner's sign-in being set up at that moment: a code an administrator makes replaces it, and the owner's setup asks them to try again.
- The CSRF store caps pending tokens per user, so an administrator's page (it refreshes every five seconds while a code is open) can't push the owner's tokens out.

## Tests

- `tests/security/owner-check.test.ts`: `checkOwner` with `admins` admits the owner and an active `system-admin`; refuses a plain user, a read-only user, an inactive or `is_active`-missing admin, a system-generated admin, a user missing from the list, `group_ids` absent or not an array, a spoofed peer or duplicated header; 503 on a failed read.
- `tests/http/nova-page-admins.test.ts`: an administrator gets 200 without the chat section, forms for code and revoke; their `generate_code` and `revoke_device` work; every owner action (`reset_registry`, every chat action) answers 403 and changes nothing, even with a valid token; a non-admin gets 403 on GET and POST; the owner keeps every section; the App's own connections can't be revoked. Updated: the old "admin is refused" tests in `nova-approve`, `nova-page-chat-users`, and the renamed `nova-page-not-owner` file.
- Bed (`scripts/e2e/testbed/nova-page-admins.mjs`): on the test bed, creates an administrator and a plain user in Home Assistant, runs the page handler with the users exactly as the bed's `config/auth/list` returns them (ingress identity simulated, the bed has no Supervisor): the owner and the administrator open it, the plain user is refused, the administrator's chat action is refused; removes both users.
