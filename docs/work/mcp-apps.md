# MCP Apps: plan cards and live status in Claude and ChatGPT

Status: active

Date: 2026-11-26. Owner: the MCP server in the CLI (`cli/mcp_tools.go`, `cli/mcp_prompts.go`, `cli/mcp_server.go`, the chat bridge `cli/mcp_serve*.go`), a new embedded view (`cli/mcpapp/`), the catalog `cli/confirm_strings.*.json`, and `docs/reference/chat-apps.md`. Target: the status card and the plan card in the 0.37 train, the rename table in 0.38. Behavior-changing, release-bound (Go code, the App image), and high-risk (next to the confirmations; one new tool). No Relay change and no change to the Home Assistant integration.

## Why

MCP Apps is the official MCP extension (stable since 2026-01-26) that lets a tool result show a small web view, a card, inside the chat. Two moments in chat apps lose people who aren't technical:

1. **The phone wait.** After an unlock or a delete, the person taps the button on the notification. If the chat still says it is waiting, they have to tell the assistant to go on (`docs/reference/chat-apps.md`, "What your phone confirms"). Few people guess that. The tap applies at once (`cli/mcp_serve_phone.go:284-287`), so a person who never comes back leaves a chat that still says "waiting" while the door is already unlocked.
2. **The preview as text.** A bigger change comes as a screen of text rows, often on a phone, and ends with "Reply yes to go ahead" (`say.yes_ask`).

The goal is comfort first: fewer chat turns, nothing to type that a person must know, and clear words. Safety keeps working in the background and adds no new step for anyone.

## Goals and non-goals

- Goals: a live status card that ends the "go on"; a plan card with one button for changes that ask for an OK in the chat; plain words in the person's language; the same safety as today; every tool stays fully useful as text.
- Non-goals: a new kind of confirmation; any change to what needs the phone; logic in the view or the Relay; setup, pairing, or phone linking in the chat (they stay on the NOVA page by design, `docs/reference/chat-apps.md`, "Good to know").

## Flows, ranked by comfort win

"Turns" counts the messages the person has to type. The click dummy (`renders/HANO-86/klickdummy.html` in the maintainer's drive, not in the repo) shows each flow, its edge cases as clickable paths, and under "Alle Zustände" every card state with the copy below.

| # | Flow | Today | With the card | Who | When |
|---|---|---|---|---|---|
| F1 | **Phone wait without "go on".** "Unlock the front door": the assistant starts the phone ask as today; its card says "Waiting for your phone" with the time left, then the outcome in the same card after the tap. | 2 turns, one of them "go on" | 1 turn, nothing typed | Use the home, Manage everything | 0.37, first |
| F2 | **Plan card with an action button.** A change that asks for an OK in the chat (a new automation, a batch of fixes, more lights than named): rows show what changes (before → after), why it asks, and one button named for the action ("Turn off"). The outcome replaces the card's content. | 2 turns, one of them "yes" | 1 turn and one tap | Manage everything | 0.37, second |
| F2p | **Plan card for a phone change.** A delete or another change that needs the phone: the card shows what changes and "Needs your phone", without a button. The assistant starts the ask as today, and the card then follows it. | preview text, then the phone | the same steps, readable | Manage everything | with F2 |
| F5 | **A code you can read.** Manage everything, no phone, codes on: the card shows the four digits large, with the time left and the catalog's note that the AI sees them. The person types the code in the chat, as today; once it is used or expired, the card hides it and shows the outcome. | the code inside the assistant's text | readable, same step | Manage everything | with F2 |
| F3 | **Rename table you can trim (#979).** One row per rename with old and new name and id. All ticked: "Rename". Unticking turns the button into "Show preview without 2", which puts that request in the person's words into the chat (the host may ask first or leave it for the person to send); the assistant plans again, and a new card says "New plan". After the rename, one button asks the same way for a preview of updating the automations that used the old names, which keeps its own OK. | up to 5 turns, three of them "yes" | 3-5 taps, nothing typed | Manage everything | 0.38 |

Cut for now: the entity or area picker and the history chart (each needs a read tool linked, so a frame next to every lookup), onboarding and pairing help (outside the chat by design; the one chat moment, "no phone linked", becomes an outcome line that names the next step), and F4, a button for Use-the-home changes from `ha_call` (D3). The #978 dead-reference table stays out of the chat for 0.37 (D1).

Noise rule: a tool links the card only when every result it returns is a decision or an outcome someone waits for. That holds for `ha_apply` and `ha_plan` and for no read tool. A tool's link is static, so every result of a linked tool may show a frame, also a refusal.

## Safety boundary

The host passes a card's tool call to our server exactly like a model call (MCP Apps specification 2026-01-26: "The Host MAY forward any message from the View … to the MCP Apps server"). The server can't tell a click from the AI, so the card has the model's power and no more. Every check stays where it is today: the level allowlist (`cli/mcp_serve_levels.go`), plans stored per user (`cli/mcp_serve.go:274-275`), expiry, recompute, and drift at apply (`cli/verb_apply.go:146-147, :208-213`), and the phone or code for changes that need them.

| Tier | What the card may do | What stays as today |
|---|---|---|
| Read | Show the result. | The level decides what may be read. |
| Ordinary (runs at once) | Show the outcome (`ha_call` isn't linked). | Runs and is checked afterwards. |
| Asks for an OK in the chat (preview) | The action button sends `ha_apply` with the plan id from the server's result: only on a click, and only when the result has `ready: true`, tier `preview`, no `channel`, no `code`, and an unexpired `expires_at`. | The same call the assistant sends after a typed "yes"; neither carries a token (`cli/verb_apply.go:31-35`). Now the person presses it while looking at the stored preview, as on the setup page (`docs/reference/safety.md:50`). Expiry and drift still refuse. Claude shows no approval prompt for calls a card makes (Claude docs, "Approval before an app tool runs"), so the button is the OK, the same step as the typed "yes". |
| Needs the phone (typed) | Show "Needs your phone"; follow the ask read-only with `ha_status` and show the outcome. No button: the assistant starts the ask, as today (`cli/mcp_args.go:180-189`). | Only the tap on the phone confirms: the requester's own linked phone, or on the standalone Relay container the phone linked to the container; no one else's phone gets the ask. A tap after the ask ended counts nowhere (its listener is gone, `cli/confirm_phone.go:131-136`), and the notification is replaced by the result. Without a phone, refused below Manage everything. |
| Needs a code (typed, chat code) | Show the code large; follow read-only; hide the code once the plan has a result or the code expired. No code field, no button. | The person types it in the chat. Codes stay Manage everything only. |
| Every tier and level | Never start an ask, never confirm a typed change, never write on load or hover, never retry a write, never follow a result's text into another plan. | The read-only switch refuses every change. |

Rules for the card, and nothing more (no extra token, nonce, origin check, or view-only confirm tool; the server couldn't verify them anyway):

1. Text from Home Assistant goes in through `textContent` only: no `innerHTML`, `insertAdjacentHTML`, `eval`, or `document.write`, checked by a test over the embedded file. Names reach the result through `sayText` already, which cuts them to 60 characters and turns `<` and `>` into brackets (`cli/confirm_say_text.go:216-231`).
2. The resource declares no `csp`, so the host's strict default applies: inline script and style only, every external fetch blocked.
3. The resource is the same file for everyone: no user, home, or plan data; it carries both label catalogs (`resources/read` answers are marked shareable, `cacheScope: public`, `cli/mcp_server.go:284`). Change status comes only from tool calls. System fonts only, and the card stores nothing in the browser.
4. Cards are drawn only from the server's tool result, never from the tool input the assistant wrote.
5. Writes happen only in a click handler. A message to the chat (`ui/message`) goes only on a click, names the change it is about, and only asks for a new preview, never "yes" or "go ahead"; after it is sent, every control on that card turns off. The host may show the person an Allow first or leave the message in the input for them to send, so the card says nothing about it having been sent. Context updates (`ui/update-model-context`) carry only `HA NOVA card, plan <id>: <outcome kind>.`, in English, nothing else; hosts may deliver them late or not at all (reported missing on Claude Desktop and Android), so no flow depends on them and the card never claims the assistant knows.
6. No server code reads whether a card is present.

## Server changes

1. **`ha_status`, a new read-only tool.** Contract below. An App without it refuses the unknown tool, so a cached card talking to an older App fails closed.
2. **Keep the last finished attempt for `ha_status`.** Today a finished chat apply hands its outcome to one caller and deletes the job (`cli/mcp_serve_apply.go:20, :280`). Also keep a display copy of the last finished attempt per Home Assistant user and exact plan id, for 15 minutes after it ended (checked when read). A finished attempt is one with an `outcome` (table below), including a no on the phone and no answer; errors a person can correct (`CODE_REQUIRED`, `CODE_MISMATCH` before the last try, `BUSY`, a snapshot failure) aren't kept, so a corrected code or a retry works through `ha_apply` as today. A new attempt on the same plan (the person asks again after a no) replaces the kept copy as soon as it starts. Copying never takes the result away from `ha_apply`, which keeps handing it out once, and never reuses `awaitJob`, which deletes the job.
3. **Result-only fields** on `ha_plan`, `ha_apply`, and `ha_status` results, in `structuredContent` and so in the text as well (stored plans don't change):
   - `lang`: the language of the result's text (`en` or `de`), so the card's labels match it.
   - `goal`: the change in a few words, the text the phone's question uses (`goal.*`, for example "Haustür entriegeln").
   - `reason` (plans): why it asks, one short line from the tier engine's reasons, without "so I need you to confirm it".
   - `button` (plans the card may act on): the action verb from `button.*` ("Ausschalten", "Umbenennen"); `app.go` when there is no single verb (a mixed batch).
   - `ready` (plans): an allowlist, not a guess. The step rules can't prove today that any preview is final (`cli/mcp_steps.go:16-79`, `cli/plan_batch.go:38-58`), so `ready` is true only for plan kinds the server lists as final previews: to start, service calls on the targets the plan names, a batch after the assistant closed it and its checks ran, and a rename after its consumer check. Never before a raw plan's copy or consumer check, never for a member of a batch still being built. Missing or anything but `true` means no button.
   - `rows` (plans and results): what the card lists, each `{name, before, after}` (F3 adds `old_id` and `new_id`), names through `sayText`, values through `confirmT`.
   - `waiting_on`, `ask_sent`, `ask_ends_at`, `tap` (waits): whose phone is asked (`own_phone` or `container_phone`; a window or form on a local client is answered within the `ha_apply` call itself, `cli/mcp_stdio_apply.go:28-31`), whether the request went out, when it ends (RFC 3339, the asking side's own end), and the one line that says how to answer (`phone.tap.*` for the person's platform, else `say.phone_waiting_how`; for the container the new `phone.tap.container`).
   - `outcome` (finished attempts): `{kind, title, line}`, with `title` and `line` already in the user's language, mapped by the server as in the outcome table.
4. **Link the card.** `mcpToolList` (`cli/mcp_tools.go`) adds `_meta.ui.resourceUri` (plus the older flat `ui/resourceUri`) to `ha_apply` and `ha_plan`; `readResource` (`cli/mcp_prompts.go`) answers the `ui://` address before the skills. One list and one resource serve the local and the chat server.

### Outcome mapping (server)

| What happened | `kind` | `title` | `line` |
|---|---|---|---|
| Sent and checked | `done` | `outcome.done` | `outcome.line.done` |
| It was already that way | `already_done` | `outcome.already_done` | `outcome.line.already_done` |
| Home Assistant took it, nothing to check it with (a script, a scene) | `sent` | `outcome.sent` | `outcome.line.sent` |
| Sent, the check after it failed | `check` | `outcome.check` | `outcome.line.check` |
| May have run: unknown outcome, `PLAN_CLAIMED`, `PLAN_NOT_FOUND` after an apply, access changed while it ran | `unclear` | `outcome.unclear` | `outcome.line.unclear` |
| Some of a batch went through | `partly` | `batch.phone.partly_title` | the matching `batch.result.*` line, with the ledger as rows |
| No on the person's own phone | `not_done` | `outcome.not_done` | `outcome.line.declined` |
| No on the container's phone | `not_done` | `outcome.not_done` | `outcome.line.declined_container` |
| No answer | `not_done` | `outcome.not_done` | `outcome.line.no_answer` |
| No phone linked for this person | `not_done` | `outcome.not_done` | `outcome.line.no_phone` |
| Busy with another change | `not_done` | `outcome.not_done` | `say.busy` |
| An earlier request still waits on the phone (`CONFIRM_PENDING`) | `not_done` | `outcome.not_done` | `outcome.line.pending` |
| Wrong code (the plan stays usable) | `not_done` | `outcome.not_done` | `outcome.line.code_mismatch` |
| Wrong code, last try (the plan is spent) | `not_done` | `outcome.not_done` | `outcome.line.code_spent` |
| Any other refusal before anything was sent | `not_done` | `outcome.not_done` | `outcome.line.nothing_changed` |
| Anything not listed | `unclear` | `outcome.unclear` | `outcome.line.unclear` |

Drift isn't an outcome: a result with `status: drift` (also one that ends a phone wait) closes the card as S10b. `PLAN_CLAIMED` and `PLAN_NOT_FOUND` stay `unclear` for the assistant; only a card that got one for its own click asks `ha_status` once more, because a typed "yes" may hold the same plan (see Edge cases).

### `ha_status` contract

- Name `ha_status`, title "Check a change", annotations `readOnlyHint: true`, `destructiveHint: false`. Description: "Read-only: tells how a change you started with ha_apply stands (still waiting, still running, or its result). It never starts, repeats, or confirms anything." Input: `{"plan_id": string}`, required, described as "The plan id from ha_plan, ha_call, or ha_apply".
- Level: Use the home and above (`chatToolLevel`), `plan_id` at Use the home (`chatArgLevel`); not a change, so it never waits for the verb lock.
- Steps: (1) under the jobs mutex, take a snapshot of the running job or the kept copy for this authenticated user and exact plan id; (2) on the chat bridge only, if the state isn't final, wait outside every lock for the job's next state change, at most 20 s. The local stdio server answers at once: it handles one call after another, so a long wait would hold up the assistant. A card's call that arrives there while the assistant's `ha_apply` still waits is answered after that call (at most 50 s, `cli/mcp_stdio_apply.go:28-31`), and `ha_status` joins the calls that never answer `BUSY` (`mcpLocklessTools`); (3) read the user's access again and check the current level still allows that job's kind, services, and targets, as `ha_apply` does after a tap (`cli/mcp_serve_phone.go:338-373`, `cli/mcp_serve_levels.go:338-359`); (4) answer from a fresh snapshot.
- Output (`schema_version: 1`, `verb: "status"`, `plan: {id}`, `lang`, `say`, `next_step`):

| `status` | Extra fields | `say` | `next_step` for the assistant |
|---|---|---|---|
| `waiting` | `goal`, `waiting_on`, `ask_sent`, `ask_ends_at`, `tap` | `say.not_run` | "Nothing has run yet. Don't call ha_apply again because of this." |
| `applying` | `goal` | `say.phone_finishing` after a tap, else `say.applying` | "Still running. Don't call ha_apply again because of this." |
| `finished` | `goal`, `outcome`, `rows` (checked values or the batch ledger) | `outcome.line` | "This is the result; tell it to the user if they ask. Never call ha_apply for this plan again because of this." |
| `changed` | `goal`, `drift` (the server's drift line) | `say.changed` | "Nothing ran: the home changed since the preview. Make a new plan only if the user still wants this." |
| `unknown` | — | `app.status_unknown` | "HA NOVA can't tell how this change stands. Never say it failed or worked; the user can check in Home Assistant." |

- It returns display fields only: never a code, a nested plan, another plan's id, the apply's own `next_step`, `before_config`, or a `say` that held a code.
- `unknown` covers every case it can't vouch for, with the same reply and the same path: no such plan, another user's plan, a result older than 15 minutes, never applied, the App restarted, or a job whose kind, services, or targets the user's current level no longer allows.
- A user the owner removed or put below Use the home gets the existing refusal (`chatStopped`); the read-only switch doesn't refuse it (it reads).
- It never sends a code, never starts an apply or an ask, never takes a job's result away from `ha_apply`, and neither reserves nor carries the one-time update hint, also not on a refusal.
- Contract change: `TestMCPToolTextStable` hashes tool names, descriptions, and argument names and descriptions, so the new tool changes it, on purpose (result fields and `_meta` don't). claude.ai keys "Always allow" per tool, so existing tools keep theirs and only `ha_status` needs one. The tools version hashes the whole tool list and the instructions, so it changes too and fires ChatGPT's one-time Refresh hint.

## The card

One file, `cli/mcpapp/view.html`: vanilla HTML/CSS/JS, embedded with `go:embed` like the setup page, about 300-500 lines and under 50 KB, with a small hand-written message bridge (handshake, tool call, message, size change, host theme). The official MCP Apps SDK was checked and stays the fallback if a host needs it. Served as `ui://ha-nova/view-<8 hex of its hash>.html` with MIME `text/html;profile=mcp-app`, so a new file gets a new address.

### States and transitions

The card picks its state from the result fields alone, never from prose. "Follow" means `ha_status` for the result's plan id: one call in flight at a time, at least 2 s between the starts of two calls, until a final state, `ask_ends_at` plus 10 s (only while not applying; then S11), or teardown. Following is for the chat bridge first: on a local client the call loop serves one call at a time, so the card there mostly shows the result `ha_apply` returns. A call that fails (no answer, a host timeout) is retried; two failures in a row show S11, and the card keeps trying every 15 s for up to 15 minutes after the ask ended, then moves on as soon as an answer comes. A slow answer isn't a failure.

**On load.** A host may draw a card again later, for example when the person reopens the chat. So before it shows a button or a code, a card for a plan (S1, S3) or a wait (S4) asks `ha_status` once and shows S0 meanwhile: `finished` shows the outcome; `waiting` or `applying` shows S4 or S5; `unknown` shows the plan or the wait, unless its `expires_at` or `ask_ends_at` has already passed, which shows S13. If that first call fails, the card shows the plan without its button or code (S1n), the wait as S11. A card for an outcome (S6-S9) draws it from the result and calls nothing.

| State | Entered when | Shows | Controls | Next |
|---|---|---|---|---|
| S0 Loading | the frame is up, no tool result yet | `app.loading` | — | another state on the result |
| S1 Plan, OK in the chat | `needs_confirmation`, tier `preview`, `ready: true`, no `channel`, no `code`, not expired | badge `app.badge.ok`, `goal`, `reason`, rows (4, then `app.show_all`), `app.valid` | the action button (`button`) | click → S5, then the `ha_apply` result's state; expiry → S10a |
| S1n Plan, not ready | as S1 but `ready` missing or not `true`, or an unknown tier or channel | `goal` if present, `app.see_reply` | — | — |
| S2 Plan, phone | `needs_confirmation`, channel `phone` or `relay` | badge `app.badge.phone`, `goal`, `reason`, rows, `app.phone_next` | — | — (static: the assistant's next `ha_apply` result shows the wait in its own card) |
| S3 Plan, code | `needs_confirmation` with `plan.code` | badge `app.badge.code`, `goal`, `say.code_lead_within`, the four digits, `app.time_left`, `say.chat_note` | — | follow (`unknown` means not used yet): `finished` → S6-S9 (the digits go); code end → S10a, without the button |
| S4 Waiting | `status: waiting_for_phone` (or `ha_status` `waiting`) | badge `app.badge.phone`; title `app.waiting` (`own_phone`) or `app.waiting_container` (`container_phone`); `app.time_left` only with `ask_ends_at`; `goal`; `tap`. While `ask_sent` is false: `app.getting_ready` instead of title, timer, and `tap` | — | follow: `applying` → S5, `finished` → S6-S9, `changed` → S10b, two failed calls → S11, `unknown` → S11u |
| S5 Working | `status: applying`, or right after a click | `app.applying`, `goal` | — | follow until `finished` → S6-S9 (never times out) |
| S6 Outcome, good | `kind` `done`, `already_done` (check mark) or `sent` (no check mark) | `outcome.title`, `goal`, `outcome.line`, rows folded | `app.details` with rows; in F3 also `app.update_users` when the result lists automations that still use the old names | the follow-up preview in a new frame |
| S7 Outcome, not done | `kind: not_done` | `outcome.title` in red, `goal`, `outcome.line` | — (never a retry for a write) | — |
| S8 Outcome, unclear | `kind` `unclear` or `check` | `outcome.title`, `goal`, `outcome.line` | — | — |
| S9 Outcome, partly | `kind: partly` | `outcome.title`, `goal`, `outcome.line`, the ledger as rows | `app.details` | — |
| S10a Expired | while the card is open, the S1 or S3 timer passes `expires_at`, or a click meets `PLAN_EXPIRED` | `goal`, `app.expired` | `app.ask_again` (S1 only): sends `app.msg.ask_again` | the new preview in a new frame |
| S10b Changed | any result with `status: drift`, or `ha_status` `changed`, for this plan | `goal`, the server's drift line (`drift.*`, else `say.changed`) | `app.ask_again` only when the drift answered the card's own click (the assistant didn't see it) | the new preview in a new frame; an old card never takes over a new plan id |
| S11 Can't tell yet | following failed twice in a row (connection lost, timeout), or the App doesn't know `ha_status` (older App) | `goal`, `app.lost` | — | keeps trying (above); an answer → its state. The phone ask itself goes on, and the assistant's own `ha_apply` still gets the result |
| S11u Unknown | `ha_status` answers `unknown` while the card follows | `goal`, `app.status_unknown` | — | — |
| S12 Unknown shape | an unknown `schema_version` or status | `app.see_reply` | — | — |
| S13 Earlier card | on load, `ha_status` says `unknown` and the plan or the ask already ended | `goal`, `app.earlier` | — | — |
| — No card | the host shows no cards, "Allow" was declined, or the file didn't load | today's text, unchanged | — | — |

A refusal is just S7 with the server's line: the card doesn't read the read-only switch or the level itself.

### Edge cases

Each case ends in a state above; nothing here adds a step for the person.

| Case | What happens | State and copy | Dummy path |
|---|---|---|---|
| Double tap on the button | The first click (or Enter or Space) turns the button off before the call goes out; a card sends at most one `ha_apply` while it lives. | S5, then the outcome | F2: tap the button twice |
| A typed "yes" and a click at once | Both `ha_apply` calls meet the same plan; the server runs it once. The card's call may get `PLAN_CLAIMED` or `PLAN_NOT_FOUND`; it then asks `ha_status` once and shows the outcome, else S8. | S6-S9, else S8 | All states |
| The plan expires while shown | The timer turns the card into S10a; a click that meets `PLAN_EXPIRED` does the same. Nothing was sent. | S10a, `app.ask_again` | F2: let it expire |
| Something at home changed | The server's drift check refuses; the card names what changed. | S10b | F2: change something at home |
| The phone answers after the ask ended | The listener is gone, so the late tap counts nowhere; the notification was already replaced by `phone.result_title` ("Nicht erledigt: Haustür entriegeln"). | stays S7 with `outcome.line.no_answer` | F1: no answer, then tap late |
| Connection lost while waiting | Two failed calls show S11; the card keeps trying and shows the outcome once an answer comes. The phone ask goes on. | S11, then S6-S9 | F1: lose the connection, then get it back |
| Two chats at once | A second phone change while one waits gets `CONFIRM_PENDING`; another change while one runs gets `BUSY`. Two cards for the same plan follow the same job and show the same outcome. | S7 with `outcome.line.pending` or `say.busy` | F1: a second chat |
| The card is drawn again later (the chat reopened) | The load check above: a finished plan shows its outcome, a running one its state, an ended one S13; a code is never shown again after its plan ended. | S6-S9, S4, S5, or S13 | F1 and F2: reopen the chat later |
| An older App behind a newer card | The App refuses the unknown `ha_status`. | S11 | All states |
| A card older or newer than the App (ChatGPT keeps cards until Refresh) | An unknown `schema_version` shows S12; a missing `ready` means no button. | S12 or S1n | All states |
| A host without cards, or "Allow" declined | Today's text. | No card | Scenario 4 |
| Another person's phone | Only the requester's own phone gets the ask, or on the standalone Relay container the container's phone; the card says which, and a no there gets its own line. | S4 with `app.waiting_container`; S7 with `outcome.line.declined_container` | F1: Relay container |
| No phone linked (Use the home) | Refused before anything is sent. | S7 with `outcome.line.no_phone` | F1: no phone linked |
| Long German names | `sayText` cuts a name at 60 characters; rows and buttons wrap, never cut or scroll sideways. | any | All states |
| Many rows | 4 rows, then `app.show_all` up to 12; above 12 the first 4 and `app.see_reply`. | S1, S2, S6, S9 | All states |
| Dark mode, larger text | The host's theme and font size; contrast at least 4.5:1. | any | the Light/Dark switch |
| The person leaves the chat while waiting | Teardown stops following; the ask goes on. Coming back is the reload case. | — | — |

### Copy

Sentences about the change come from the server (`goal`, `reason`, `tap`, `outcome.title`, `outcome.line`), already localized from the catalog with names through `sayText`. The card shows the short fields, not the full `say`, so it doesn't repeat the assistant's text, and capitalizes the first letter of `goal` where it starts a line ("die Automation „Flurlicht nachts“ löschen" becomes "Die Automation …"). The card's own labels are a new `app.*` block in `cli/confirm_strings.{en,de}.json` (the catalog's tests then cover both languages), built into the file; the card picks `lang` from the result, else the host's locale, else English. The card never shows plan ids, service names, raw states, or the words typed, tier, or apply; ids appear only in F3, where they are what changes, and "New plan" is the one place the word plan appears.

New card labels (`app.*`):

| Key | English | German |
|---|---|---|
| `app.loading` | Getting this ready… | Wird vorbereitet… |
| `app.badge.ok` | Needs your OK | Braucht dein OK |
| `app.badge.phone` | Needs your phone | Braucht dein Handy |
| `app.badge.code` | Type the code in the chat | Code im Chat eintippen |
| `app.go` | Go ahead | Los |
| `app.applying` | Working on it… | Wird ausgeführt… |
| `app.waiting` | Waiting for your phone | Warte auf dein Handy |
| `app.waiting_container` | Waiting for the container's phone | Warte auf das Handy am Container |
| `app.getting_ready` | Your phone gets the request in a moment. | Gleich kommt die Anfrage auf dein Handy. |
| `app.time_left` | {time} left | Noch {time} |
| `app.valid` | one: Valid for {count} more minute · other: Valid for {count} more minutes | one: Noch {count} Minute gültig · other: Noch {count} Minuten gültig |
| `app.phone_next` | Look for a notification on your phone in a moment. | Gleich kommt eine Benachrichtigung auf dein Handy. |
| `app.show_all` | Show all {count} | Alle {count} zeigen |
| `app.show_less` | Show less | Weniger zeigen |
| `app.details` | Details | Details |
| `app.see_reply` | The details are in the reply. | Die Details stehen in der Antwort. |
| `app.expired` | This preview has expired. | Diese Vorschau ist abgelaufen. |
| `app.ask_again` | Show a fresh preview | Vorschau neu anzeigen |
| `app.lost` | Can't tell yet how this ended. Ask here in the chat for the result. | Noch unklar, wie es ausging. Frag hier im Chat nach dem Ergebnis. |
| `app.status_unknown` | HA NOVA can't tell how this change stands. Check in Home Assistant. | HA NOVA kann nicht sagen, wie es um diese Änderung steht. Sieh in Home Assistant nach. |
| `app.earlier` | This card is from earlier. Home Assistant shows how things stand now. | Diese Karte ist von früher. Wie es jetzt steht, zeigt Home Assistant. |
| `app.rename_hint` | Keep ticked what you want to rename. Only names and ids change, not the devices. | Lass nur angehakt, was du umbenennen willst. Nur Namen und IDs ändern sich, nicht die Geräte. |
| `app.ask_without` | Show preview without {count} | Vorschau ohne {count} anzeigen |
| `app.none_ticked` | Nothing ticked | Nichts ausgewählt |
| `app.new_plan` | New plan | Neuer Plan |
| `app.uses_old` | one: {count} automation uses the old names: {names}. · other: {count} automations use the old names: {names}. | one: {count} Automation nutzt die alten Namen: {names}. · other: {count} Automationen nutzen die alten Namen: {names}. |
| `app.update_users` | one: Preview updating the automation · other: Preview updating the {count} automations | one: Vorschau: Automation anpassen · other: Vorschau: die {count} Automationen anpassen |
| `app.msg.ask_again` | Show me the preview for “{goal}” again. | Zeig mir die Vorschau für „{goal}“ noch einmal. |
| `app.msg.without` | Leave out {names} and show me the new preview. | Lass {names} weg und zeig mir die neue Vorschau. |
| `app.msg.update_users` | one: Show me a preview of updating the automation that uses the old names. · other: Show me a preview of updating the {count} automations that use the old names. | one: Zeig mir eine Vorschau, wie die Automation mit den alten Namen angepasst wird. · other: Zeig mir eine Vorschau, wie die {count} Automationen mit den alten Namen angepasst werden. |

New server lines:

| Key | English | German |
|---|---|---|
| `outcome.line.declined` | You said no on your phone. Nothing changed. | Du hast auf dem Handy abgelehnt. Es wurde nichts geändert. |
| `outcome.line.declined_container` | The phone linked to your NOVA Relay container said no. Nothing changed. | Am Handy, das mit deinem NOVA-Relay-Container verbunden ist, wurde abgelehnt. Es wurde nichts geändert. |
| `phone.tap.container` | The request went to the phone linked to your NOVA Relay container. Whoever has that phone answers it. | Die Anfrage ging an das Handy, das mit deinem NOVA-Relay-Container verbunden ist. Wer dieses Handy hat, beantwortet sie. |
| `outcome.line.no_phone` | This needs a phone linked to HA NOVA for you. The Home Assistant owner links it on the NOVA page. Nothing changed. | Dafür braucht es ein Handy, das für dich mit HA NOVA verbunden ist. Der Eigentümer von Home Assistant verbindet es auf der NOVA-Seite. Es wurde nichts geändert. |
| `outcome.line.pending` | An earlier request still waits on your phone. Answer that one first; this one wasn't sent. | Auf deinem Handy wartet noch eine frühere Anfrage. Beantworte zuerst diese; diese hier wurde nicht gesendet. |
| `outcome.line.code_mismatch` | The code didn't match. Nothing changed. Type the code from above again. | Der Code passt nicht. Es wurde nichts geändert. Tippe den Code von oben noch einmal ein. |
| `outcome.line.code_spent` | The code was wrong too often, so this preview no longer works. Nothing changed. Just ask again. | Der Code war zu oft falsch, deshalb gilt diese Vorschau nicht mehr. Es wurde nichts geändert. Frag einfach noch einmal. |

Existing keys used unchanged: `outcome.done`, `outcome.already_done`, `outcome.sent`, `outcome.check`, `outcome.unclear`, `outcome.not_done` and their `outcome.line.*` ("Erledigt" / "Hat geklappt; HA NOVA hat es in Home Assistant geprüft.", "Nicht erledigt" / "Home Assistant hat keine Antwort erhalten. Es wurde nichts geändert." …), `batch.phone.partly_title` with `batch.result.*`, `say.busy`, `drift.*` and `say.changed`, `say.code_lead_within`, `say.chat_note`, `say.not_run`, `phone.tap.*`, `phone.result_title`, `goal.*`, `button.*`. The phone notification itself doesn't change.

### Layout and size

- The card fills the width the host gives, up to 560 px, and works down to a 320 px screen. Body text 16 px, secondary 14-15 px, line height 1.5. One calm accent color for badges and the button, green only for the check mark, red only for "Not done". Light and dark come from the host's theme and style variables (`hostContext`), updated on `host-context-changed`; contrast at least 4.5:1.
- **Phone width (card under 480 px):** 14 px padding; the button full width, at least 44 px tall; a row puts the change under the name; long values wrap; a whole rename row is the tap target for its box; the host's safe-area insets are respected.
- **Desktop width:** 16 px padding; the button right-aligned next to "Valid for … minutes"; a row is one line, name left, change right.
- **Heights** are targets that phase 0 measures, never clipping: the card reports its own height with `size-changed` and never scrolls inside. A status (S4, S5, S11) about 150 px; an outcome (S6-S9) about 160 px with rows folded; a plan card (S1-S3) about 450 px with 4 rows. "Show all" opens up to 12 rows in place; above 12 the card shows the first 4 and `app.see_reply`, or asks the host for full screen where it offers it.
- Motion: one short fade when the state changes, none with reduced motion, and no looping animation: a waiting card counts its time down instead. State changes go to an `aria-live` region; the button has a visible focus ring.
- Text: names and values wrap (`overflow-wrap: anywhere`), never cut by the card or scrolled sideways; the card follows the host's font size.

## Text fallback

Hosts without cards (terminal clients, a declined "Allow", a failed load) see today's text, unchanged:

- F1: the assistant tells `say.phone_waiting` ("Ich warte, bis du auf deinem Handy die Benachrichtigung zu dieser Änderung beantwortest …") and keeps calling `ha_apply` in the same answer; if its answer ends first, the person asks again once they tapped.
- F2: the preview in words, ending with `say.yes_ask` ("Antworte mit Ja, um fortzufahren."); the person types "yes".
- F5: `say.code_lead_within` with the code and `say.chat_note`; the person types the code.
- F3: the rename preview in words; the person names what to leave out.
- A no-phone refusal at Use the home and the other S7 cases: the same `outcome.line` in the assistant's text.
- ChatGPT after an update: the existing one-time hint to open HA NOVA under Plugins and choose Refresh; Claude users do nothing.

`content[]` stays byte-identical except for the additive result fields above. A golden test compares hint-free results with and without the card link; the update hint is tested on its own. The card adds nothing the text lacks, except the live status after the assistant's answer ended. Nothing falls back to a weaker confirmation, and a card error never turns into "confirm with yes".

## Hosts

Checked from the hosts' own docs, 2026-11-26; "reported" means a community report only. Phase 0 tests the flows before the docs claim them.

| Host | Shows the card | Notes |
|---|---|---|
| Claude on the web and desktop | yes | Asks "Allow / Always allow" before the first card (one line in `chat-apps.md`); calls a card makes run without a prompt; a message from a card may need the person's OK or Send. Desktop documents cards for local servers too, but current builds are reported not to read them (phase 0). |
| Claude on iOS and Android | yes | The connector is added on the web or desktop first, as today. No side panel or full screen on phones. |
| ChatGPT on the web | yes, once MAR-150 is fixed | Keeps tools and cards until the person chooses Refresh. |
| ChatGPT on a phone | unknown for HA NOVA | Reported bugs hide or stall cards (one whose tool runs after another tool in the same answer, polling paused); checked after MAR-150. |
| Other desktop apps with MCP Apps support | some, some behind a setting | Local `ha-nova mcp`; whether their cards can call tools is unverified. |
| Terminal clients | no, text only | Unchanged. |

The card never talks to Home Assistant or our server directly: all data comes from tool results or host-proxied tool calls, through the same connector address, sign-in, and level checks as today. The integration forwards every request body to the bridge (`nova/integration/ha_nova/__init__.py:99-114`), and the Go bridge answers `resources/read`, so the Relay and the integration stay unchanged. Chat users get the card with the App update that carries the bridge, local clients with the CLI release.

**MAR-150** (ChatGPT shows no tools at Markus's home): the ChatGPT rows need it fixed; without tools, ChatGPT never sees the card link. A fix is necessary, not proof that the card works there, so ChatGPT is verified after it. Claude and the local clients ship regardless.

## Acceptance criteria

Phase 0 (prototype; the remote connector in Claude first, because the local path is reported broken in current Claude builds and serves one call at a time: on the test bed with the local `ha-nova mcp` where Claude Desktop reads the card, and the remote path over a public address and Claude on a phone as a handover to Markus on a throwaway Home Assistant, never his home):

- P1: measure S4, S6, and S1 heights in Claude on the web and on a phone, and whether a new card makes the chat jump; set the final targets from that.
- P2: confirm that calls a card makes run without a prompt in Claude, as its docs say, and record when "Allow" for showing cards comes. If card calls prompt after all, F1 doesn't ship as designed (back to review).
- P3: a card call that waits 20 s doesn't time out in Claude; two cards following at once and the assistant's own `ha_apply` calls don't collide.
- P4: `ui/message` from a click: record whether the host asks first, leaves it in the input, or sends it, and whether the assistant answers it.
- P5: reopen an old chat before and after a plan finished or expired, and after an update: does the host draw the card again, replay its result, and let it call tools; record when teardown comes.

Implementation (tests on the CLI, then the end-to-end agent run on the test bed):

- F1: a tap, a no, and no answer each end in the right outcome in the card without a typed message; the assistant's own next `ha_apply` still gets its result once; no second notification is ever sent (counted on the test bed); following stops at `ask_ends_at` plus 10 s unless applying, or on teardown; a tap after the ask ended changes nothing; after two failed calls the card shows S11 and then the outcome once calls work again; the container's phone shows its own title and no-line.
- Reload: a card drawn again from the stored result shows the outcome for a finished plan, never a button for it, never a code after its plan ended, and S13 once the plan or ask ended unknown.
- F2: the button sends exactly one `ha_apply` with the result's plan id and turns off on the first click, also on a double click or a held Enter; a click racing a typed "yes" ends in the outcome, not S8, when the plan ran; it appears only with `ready: true`, tier `preview`, no channel, no code, not expired; an expired plan shows S10a and sends nothing; a drifted plan shows S10b, also when the drift ends a phone wait.
- F2p and F5: no control on the card sends anything; the code shows exactly as in the text and disappears once the plan has a result or the code expired.
- F3: unticking never changes what the button sends; "Show preview without n" sends one message and turns off every control on that card; a rename that needs the phone shows S2.
- `ha_status`: never starts or repeats an apply or an ask (job count and notification count unchanged); never returns a code, a nested plan, another plan id, or `before_config`; answers `unknown` the same way for another user's plan, an old result, an unknown id, and a job above the user's current level; keeps the last finished attempt for 15 minutes and drops it when a new attempt starts; leaves corrected codes and retries working; refuses after the user is removed; never reserves or carries the update hint; answers at once on stdio and never answers `BUSY` there.
- Card file: no `innerHTML` and friends (a test greps the embedded file); declares no `csp`; byte-identical for two users; carries every `app.*` key in both languages.
- Compatibility: `content[]` golden for hint-free results; existing tool text unchanged; the tools version changes; an older App answers the card's `ha_status` with a refusal and the card shows S11.

## Phases and effort

| Phase | What | Effort | Lands |
|---|---|---|---|
| 0. Prototype | P1-P5 above with a stub card. | S | before 1b |
| 1a. Server | `ha_status`, the kept last attempt, the result fields, the outcome mapping, the new `outcome.line.*` keys, with their tests. | M, ~200 Go lines | 0.37 |
| 1b. Status card (F1) | The card with S0, S4-S9, S11-S13 and the load check on `ha_apply`; the `app.*` keys; in `chat-apps.md` the "Always allow" line and one screenshot of each card state a person meets (S1, S3, S4, S6, S7), so nobody meets a card for the first time unprepared. | M, ~60 Go lines plus the card | 0.37, with the train's one App bump |
| 1c. Plan card (F2, F2p, F5) | S1-S3, S10 on `ha_plan`; `rows`, `ready`, `button`. | M | 0.37 if 1b is in time, else 0.38 |
| 2. Rename table (F3) | `old_id`/`new_id` rows for raw renames; the tick table. ChatGPT check after MAR-150. | M-L | 0.38 |

At build time: two adversarial lenses (fail-open; UX and agent guidance) over the final diff, the end-to-end agent run, and the Codex round. Visual work follows the impeccable skill with before/after screenshots.

## Risks

- **Host differences.** Heights, timeouts, redrawing old chats, and what a card message does are partly undocumented. Phase 0 measures them; the text path covers any gap.
- **Stacked frames.** While a person waits, the assistant may still call `ha_apply` again in the same answer, and each result may show a frame that follows the same plan. Accepted for v1; P3 checks it.
- **The first prompt comes at a tense moment.** With F1 first, a household member's first card is likely their first unlock, so Claude's "Allow" comes right then. One line in `chat-apps.md` ("When Claude asks to show HA NOVA's cards, choose Always allow"); declining keeps the text path.
- **Our own message bridge.** We own a small bridge instead of a library; the SDK stays the fallback.
- **ChatGPT keeps old cards.** Each new file fires the Refresh hint again, so card changes land at most once per train.

## Decisions

Markus decided D1-D4 on 2026-11-26 as recommended: „Alle Empfehlungen so übernehmen“.

- **D1. #978 in chat apps: no table in 0.37.** The full dead-reference sweep stays computer-only, because bringing it into chat needs a new `ha_get` argument, which resets every claude.ai user's "Always allow" for `ha_get` once. The chat keeps its shorter static check as text; the person names the fixes, and F2 makes confirming them one tap. The full sweep in chat, with a pick table on its result only, is a 0.38 candidate if people ask.
- **D2. No code that only the card shows.** The server can never know the person saw the card, and hiding the code from the text breaks setups without a phone that work today.
- **D3. `ha_call` cards are decided after phase 0 (P1).** Linking it would save one typed "yes" on Use-the-home changes that ask for an OK, but may show a frame next to every "light on".
- **D4. The button names its action** from the catalog (`button.*`: "Ausschalten", "Umbenennen"), with "Los" (`app.go`) only where there is no single verb; and no code field or code button in the card.

## Open questions

Only real hosts can answer: P1-P5 above, ChatGPT after MAR-150, and whether the ChatGPT phone app still hides cards in chained calls.

Kept as today, not questions: the phone for unlocks, doors, gates, garage doors, the alarm, and deletes; codes only at Manage everything; the separate OK for updating automations after a rename (F3 makes it one tap); no code field and no code button in the card.
