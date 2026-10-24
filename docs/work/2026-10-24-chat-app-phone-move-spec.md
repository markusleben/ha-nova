# Change or stop the chat-app phone (2026-10-24)

Status: active

Target: 0.28. Parents: `docs/work/2026-10-19-chat-bridge-contract.md`, `docs/work/2026-10-17-human-confirmation-channels-spec.md` §7.9 (the computer's link, move, and stop). Used by `docs/work/2026-10-24-chat-apps-household-spec.md` for every user's phone.

## Problem

- Once chat apps have a phone, nobody can change it. Setup's "Use another phone" moves only this computer and then says "Chat apps still ask the phone they had before. You can't move them to the new phone." (`setup.chat.phone_kept`, `cli/setup_page_chat.go:115-126`, `cli/setupweb/strings.en.json:558`).
- The CLI hands the owner's phone over only when the App has none (`handOverOwnerPhone`, `cli/chat_apps.go:216-238`). That check is on the client side only: the App's `owner_phone` field overwrites or (`null`) deletes the link with no question to the old phone (`nova/src/runtime/chat-apps.ts:382-390`, `nova/src/http/handlers/chat-apps.ts:69-71,104-106`).
- On a computer, a move or a stop needs a tap on the old phone, or the confirmation window when that phone can never answer again (`allowPhoneChange`, `cli/confirm_phone_link.go:270-336`). Chat apps have no such rule.

## Goal

- The owner can move chat apps to another phone, or stop phone confirmation for chat apps, from the NOVA page and from setup.
- Same rule as the computer: the phone linked now says OK first. A phone that can never answer again is replaced only by a person on the NOVA page.
- One place writes the chat-app links, so two writers can't race.

## Non-goals

- Syncing the computer's link and the chat-app link (two stores stay, by design, spec §11.4).
- One tap that moves both the computer and chat apps (open question 2).
- A phone change started from the chat itself.

## Design

### The bridge owns the links

- New control endpoints on the bridge: `GET /control/v1/phones?user=<id>` (that user's eligible phones and current link), `POST /control/v1/phone` (start a job), and `GET /control/v1/phone/<job>`. They are served on a second listener bound to loopback only (`--control-listen 127.0.0.1:<port>`): the bridge's `/mcp` listener is on `0.0.0.0:8794` (`nova/src/runtime/chat-apps.ts:161`), which Home Assistant Core and other Apps on the internal network can reach. The App and the bridge share the container, so only the App reaches the control listener.
- Its own secret too: a second 256-bit file `control-secret`, made by the App with the first one (and at boot when chat apps are already on, e.g. after updating from 0.27), passed as `--control-secret-file`. Home Assistant never holds it (the `/mcp` secret sits in Home Assistant's config entry, which any administrator with file access can read). It is in `/data`, so App backups hold it, as they hold the `/mcp` secret.
- The bridge becomes the only writer of `phone-links.json`. The App's `setOwnerPhone` goes away; the App calls the control endpoint instead.

### Actions

`{"user": "<HA user id>", "action": "adopt" | "move" | "stop" | "drop", "phone": {service, device_id, name}?, "override": false}`

- **adopt**: the first chat-app phone, handed over from a computer link that already got the user's tap (today's `owner_phone`). Accepted only while the user has no link; otherwise 409 `PHONE_EXISTS`. No question is sent.
- **move**: (1) the phone linked now asks "Move HA NOVA chat-app confirmations to “<new phone>”?" [Move]. (2) Then the new phone gets the link question [Allow]. The tap must arrive as `user`, who must be an administrator (the computer's eligibility rule, `cli/confirm_phone_setup.go:98`). (3) The link is written only while the user's entry is still the one approved (compare, then atomic write), as `setPhoneLink` does on a computer.
- **stop**: the phone linked now asks "Stop HA NOVA chat-app confirmations on this phone?" [Stop]. Then the entry is deleted. Chat apps then use a chat code (if the owner allows codes) or refuse typed changes.
- **drop**: deletes a link with no question, accepted only for a user who is no longer in `allowed_users` (the owner removed them; household spec). The bridge also ignores any link of a user not on the list, so a failed drop can't come back to life.
- **No link yet, "move"**: same as a first link: only the new phone's tap.
- **The linked phone can never answer again** (removed from Home Assistant, its user no longer an administrator or now the Relay's user): the bridge refuses with `OLD_PHONE_GONE`, unless `override: true`. The App sends `override: true` only from the NOVA page form (owner's browser session, single-use CSRF token), never from the machine API that setup uses. That page click plays the part the confirmation window plays on a computer: a chat-app model can't press it.
- A phone that is only unreachable right now (no tap in time, notification failed, its app signed out) is not "gone": nothing changes, and the page says what to check, including the way out the computer gives (`cli/confirm_phone_link.go:308`): remove that phone in Home Assistant, after which it counts as gone.
- One job per user at a time (409 `PHONE_CHANGE_RUNNING`). A job ends after the asks' own time limits (90 s each), then reports `done`, `declined`, `timeout`, `gone`, `rejected` (tap from someone else), or `changed` (the link changed meanwhile). Jobs live in the bridge's memory: after a bridge restart the job id answers `lost` (a late tap then counts nowhere, which is safe) and the page says to try again.

### App and page

- NOVA page: next to each user's phone, [Change] and [Stop]. [Change] lists that user's eligible phones (`GET /control/v1/phones`: the same template read as the computer, `phoneTemplate`, `cli/confirm_phone_setup.go:20-36`). While a job runs, the page refreshes every 3 s (`<meta http-equiv="refresh">`, as pairing does, `nova/src/http/handlers/nova-page-view.ts:158-161`).
- App machine API (`/chat-apps/v1/state`, used by setup): `owner_phone` is accepted only while the owner has no link (now enforced in the App, not only in the CLI); `owner_phone: null` is refused with a pointer to the stop flow. This changes the contract text (`docs/work/2026-10-19-chat-bridge-contract.md`, "Owner action"). Shipped 0.27 CLIs never send `null` (`cli/chat_apps.go:234,346`); one that loses a race with the page now gets 409 and shows its existing `setup.chat.problem_phone` line. A new field `owner_phone_move` starts a move job; setup polls `GET` for its result.
- Setup: when chat apps are on and the user picks "Use another phone", the row offers "Move chat apps to “<phone>” too" in place of `setup.chat.phone_kept`. Yes starts the move job (old chat-app phone first, then a link question for chat apps on the new phone). The offer needs the owner session, which exists only after a sign-in pairing in this run (contract, "Owner session"); without it the row points to the NOVA page (re-runs today show `setup.chat.problem_phone`, `cli/setup_page_chat.go:102-104`).

### CLI

- No new command. `ha-nova chat-apps status` stays sign-in free, so it can't read the App's `/data` and doesn't name the chat-app phone; the NOVA page does.

## Security

- The old phone's OK guards a move and a stop, as on a computer. Stopping is a downgrade (codes the model can see), so it needs the same OK.
- The only way around a gone phone is a person on the NOVA page, who is signed in to Home Assistant as the owner and can already unlock doors there. A chat-app model can't reach that page. An agent that drives the owner's desktop browser could click it; that is the same class as the computer's confirmation window (T3, stated, not solved).
- The control listener is loopback-only and has its own secret, which Home Assistant never sees, so holding the `/mcp` secret or sitting on the internal network is not enough to change a link.
- A tap counts only from the link's own user while they are an administrator; the owner can't tap for another user's phone (household spec).
- `adopt` can't replace a link, so a lost answer or a replayed hand-over never overwrites one.
- Fail closed: an unreadable `phone-links.json` refuses every action; a re-read after the tap that fails leaves the link as it was.

## UX copy (draft)

Old phone (move): **Move HA NOVA chat-app confirmations?** "Unlocks and deletes from Claude and ChatGPT will ask “Anna's Pixel” instead of this phone." [Move] [Cancel]

Old phone (stop), chat codes on: **Stop HA NOVA chat-app confirmations?** "Claude and ChatGPT will show a four-digit code instead. Your AI assistant can see that code too." [Stop] [Cancel]

Old phone (stop), chat codes off: **Stop HA NOVA chat-app confirmations?** "Unlocks and deletes from Claude and ChatGPT will stop working until you link a phone again." [Stop] [Cancel]

New phone: **Use this phone for HA NOVA in chat apps?** "Unlocks and deletes from Claude and ChatGPT will ask you here." [Allow] [Cancel]

NOVA page, waiting: "Check “Markus's iPhone” first and tap Move. Then “Markus's Pixel” asks too."

NOVA page, old phone gone: "“Markus's iPhone” can't answer anymore (it was removed from Home Assistant). You can link a new phone here without it." [Link a new phone]

NOVA page, results:
- "Done. Chat apps now ask “Markus's Pixel”."
- "Nothing changed. It was cancelled on “Markus's iPhone”."
- "Nothing changed. No one tapped in time. Open the Home Assistant app on that phone and try again."
- "Nothing changed. “Markus's iPhone” didn't answer. If that phone is lost, remove it in Home Assistant (Settings → Devices & services → Mobile App). Then you can link a new phone here."
- "Something interrupted this. Nothing changed. Try again."

Setup, step 3 (replaces `setup.chat.phone_kept`): "Chat apps still ask “Markus's iPhone”. Move them to “Markus's Pixel” too?" [Move chat apps too] [Keep]

## Test plan

- Go (`cli/mcp_serve_phone_test.go`, fake Home Assistant): move needs the old tap, then the new tap from the same user; decline, timeout, rejected tap, and a link changed meanwhile change nothing; stop needs the old tap; a gone old phone refuses without `override` and links with it; `adopt` refuses when a link exists; `drop` refuses a user still on the list; a link of a user not on the list is ignored; one job per user; an unknown job id after restart answers `lost`; wrong or missing control secret → 401; `/control` is not served on the `/mcp` listener, and the control listener refuses a non-loopback bind.
- TS: `owner_phone` refused when a link exists; `owner_phone: null` refused; the page sends `override` only from the CSRF form; the machine API can't.
- Setup golden: the new step 3 line and the move offer replace `phone_kept`.
- Test bed phone lane (`scripts/e2e/testbed/`, #851): move and stop with two simulated Companion devices.

## Open questions for the maintainer

1. Override for a gone phone from the NOVA page only (default), or also from setup's page?
2. One tap to move both the computer and chat apps when they use the same phone? Needs the bridge to trust the computer's tap; default: two asks.
3. Keep `owner_phone` in the App API for 0.27 CLIs (default: yes, unchanged for them), or drop it once 0.28 ships?
