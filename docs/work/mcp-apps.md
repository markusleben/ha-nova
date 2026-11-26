# MCP Apps: plan cards and live status in Claude and ChatGPT

Status: active

Date: 2026-11-26. Owner: the MCP server in the CLI (`cli/mcp_tools.go`, `cli/mcp_prompts.go`, `cli/mcp_server.go`, the chat bridge `cli/mcp_serve*.go`), a new embedded view (`cli/mcpapp/`), the catalog `cli/confirm_strings.*.json`, and `docs/reference/chat-apps.md`. Target: the status card and the plan card in the 0.37 train, the rename table in 0.38. Behavior-changing, release-bound (Go code, the App image), and high-risk (next to the confirmations; one new tool). No Relay change and no change to the Home Assistant integration.

## Why

MCP Apps is the official MCP extension (stable since 2026-01-26) that lets a tool result show a small web view inside the chat. Two moments in chat apps lose people who aren't technical:

1. **The phone wait.** After an unlock or a delete, the person taps the button on the notification. If the chat still says it is waiting, they have to tell the assistant to go on (`docs/reference/chat-apps.md`, "What your phone confirms"). Few people guess that. The tap applies at once (`cli/mcp_serve_phone.go:284-287`), so a person who never comes back leaves a chat that still says "waiting" while the door is already unlocked.
2. **The preview as text.** A bigger change comes as a screen of text rows, often on a phone, and ends with "reply yes to go ahead".

The goal is comfort first: fewer chat turns, nothing to type that a person must know, and clear words. Safety keeps working in the background and adds no new step for anyone.

## Goals and non-goals

- Goals: a live status card that ends the "go on"; a plan card with one button for changes that ask for an OK in the chat; plain words in the person's language; the same safety as today; every tool stays fully useful as text.
- Non-goals: a new kind of confirmation; any change to what needs the phone; logic in the view or the Relay; setup, pairing, or phone linking in the chat (they stay on the NOVA page by design, `docs/reference/chat-apps.md`, "Good to know").

## What a user sees, ranked by comfort win

"Turns" counts the messages the person has to type.

| # | Flow | Today | With the card | Who | When |
|---|---|---|---|---|---|
| F1 | **Phone wait without "go on".** "Unlock the front door": the card says "Waiting for your phone" with the time left, then "Front Door is unlocked" in the same card after the tap. | 2 turns, one of them "go on" | 1 turn, nothing typed | Use the home, Manage everything | 0.37, first |
| F2 | **Plan card with "Go ahead".** A new automation, a batch of fixes, a dashboard edit: rows show what changes (before → after), why it asks, and one button; the outcome appears in the same card. A change that needs the phone shows "Needs your phone" and continues as F1. | 2 turns, one of them "yes" | 1 turn and one tap (two where the person keeps Claude's own "Needs approval" on, by their choice) | Manage everything | 0.37, second |
| F5 | **A code you can read.** Manage everything, no phone, codes on: the card shows the four digits large, with the time left and the catalog's own note that the AI can see them. The person still types the code in the chat. | the assistant may reword it | readable, same step | Manage everything | with F2 |
| F3 | **Rename table you can trim (#979).** The rename map as rows, old and new name and id; all ticked: "Go ahead" applies a plan that asks for an OK in the chat (a rename that needs the phone shows "Needs your phone"). Unticking turns the button into "Ask again without 2", which sends that request in the person's words, and the assistant plans again. After the rename, one button asks to update the automations that used the old names. | up to 5 turns, three of them "yes" | 3 taps | Manage everything | 0.38 |
| F4 | "Go ahead" for changes from `ha_call` that ask for an OK (for example "everything off in the kitchen"). | one short "yes" | one tap | Use the home | open, D3 |

Cut for now:

- **Entity or area picker, history chart.** Both need a read tool linked, so a frame next to every lookup. "Kitchen Ceiling or Counter Strip?" in text works fine.
- **Onboarding and pairing help.** They happen outside the chat. The one chat moment, "no phone linked", is a refusal whose text already names the fix; the outcome card shows that text.
- **Dead-reference table (#978)** is a scope decision (D1): chat apps can't run the full sweep today, and the chat's shorter static check is model text.

Noise rule: a tool links the view only when every result it returns is a decision or an outcome someone waits for. That holds for `ha_apply` and `ha_plan` and for no read tool. A tool's link is static, so every result of a linked tool may show a frame, also a refusal.

## Safety boundary

The host passes a view's tool call to our server exactly like a model call (MCP Apps specification 2026-01-26: "The Host MAY forward any message from the View … to the MCP Apps server"). The server can't tell a click from the AI, so the card has the model's power and no more. Every check stays where it is today: the level allowlist (`cli/mcp_serve_levels.go`), plans stored per user (`cli/mcp_serve.go:274-275`), expiry, recompute, and drift at apply (`cli/verb_apply.go:146-147, :208-213`), and the phone or code for changes that need them. A click is never the confirmation of a change that needs the phone or a code.

| Tier | What the card may do | What stays as today |
|---|---|---|
| Read | Show the result. | The level decides what may be read. |
| Ordinary (runs at once) | Show the outcome (`ha_call` isn't linked). | Runs and is checked afterwards. |
| Asks for an OK in the chat (preview) | "Go ahead" sends `ha_apply` with the plan id from the server's result, only on a click, only when the server marks the plan ready to show (Design 4). | The same call the assistant sends after a typed "yes"; neither carries a token (`cli/verb_apply.go:31-35`). The person now presses it while looking at the stored preview, as on the setup page (`docs/reference/safety.md:50`). Expiry and drift still refuse; Claude's own "Needs approval" still asks where the person chose it. |
| Needs the phone (typed) | Show "Needs your phone"; start the ask only on a click; then follow it read-only with `ha_status` and show the outcome. Following never starts, repeats, or applies anything. | Only the tap on the phone confirms: the requester's own linked phone, or on the standalone Relay container the phone linked to the container. Without one, refused below Manage everything. |
| Needs a code (typed, chat code) | Show the code large. The card never sends a code. | The person types it in the chat. Codes stay Manage everything only. |
| Every tier and level | Never confirm a typed change, never write on load or hover, never retry a write, never follow a result's text into another plan. | The read-only switch refuses every change. |

Rules for the card, and nothing more (no extra token, nonce, origin check, or view-only confirm tool: the server couldn't verify them anyway):

1. Text from Home Assistant goes in through `textContent` only: no `innerHTML`, `insertAdjacentHTML`, `eval`, or `document.write`, checked by a test over the embedded file. Otherwise a device named with markup could send a message in the person's words and fake a "yes" (it still couldn't tap a phone).
2. The resource declares no `csp`, so the host's strict default applies: inline script and style only, every external fetch blocked.
3. The resource is the same file for everyone: no user, home, or plan data. It carries both label catalogs (`resources/read` answers are cached as public, `cli/mcp_server.go:284`). Change status comes only from tool calls.
4. Cards are drawn only from the server's tool result, never from the tool input the assistant wrote.
5. Writes happen only in a click handler. Messages to the chat (`ui/message`) go only on a click and say what was clicked; context updates (`ui/update-model-context`) carry only facts the result states, such as "Front Door: unlocked", never "approved". Hosts may deliver them late, so the card never claims the assistant knows.
6. No server code reads whether a card is present.

Texts: every sentence about the change or its status is the server's `say`, which already comes from the confirmation catalog through `sayText`. Button and frame labels (about 14: "Go ahead", "Needs your phone", "Waiting for your phone", "Show all {n}", "This is out of date", "Ask again"…) are a new `app.*` block in `cli/confirm_strings.{en,de}.json`, built into the file. The card never shows plan ids, service names, raw states, or the words typed, preview, tier, plan, or apply.

## Hosts

Checked from the hosts' own docs, 2026-11-26; "reported" means a community report only. Phase 0 tests the flows before docs claim them.

| Host | Shows the card | Notes |
|---|---|---|
| Claude on the web and desktop | yes | Asks "Allow / Always allow" before the first card (a host prompt; one line in `chat-apps.md`). Desktop also shows it for the local `ha-nova mcp`. Whether it asks again for calls the card makes is unverified. |
| Claude on iOS and Android | yes | The connector is added on the web or desktop first, as today. |
| ChatGPT on the web | yes, once MAR-150 is fixed | Keeps tools and cards until the person chooses Refresh. |
| ChatGPT on a phone | reported yes; changes stay blocked by ChatGPT | A reported bug hides a card whose tool runs after another tool in the same answer; status unknown. |
| Other desktop apps with MCP Apps support | some, some behind a setting | Local `ha-nova mcp`; whether their cards can call tools is unverified. |
| Terminal clients | no, text only | Unchanged. |

Reachability: the card never talks to Home Assistant or our server directly. All data comes from tool results or host-proxied tool calls, through the same connector address, sign-in, and level checks as today. One inline file serves remote chat apps and the local `ha-nova mcp`, and no host needs a public address for it. The integration forwards every request body to the bridge (`nova/integration/ha_nova/__init__.py:99-114`), and the Go bridge answers `resources/read`, so the Relay and the integration stay unchanged. Chat users get the card with the App update that carries the bridge, local clients with the CLI release.

**MAR-150** (ChatGPT shows no tools at Markus's home): the ChatGPT rows need it fixed; without tools, ChatGPT never sees the card link. A fix is necessary, not proof that the card works there: ChatGPT is verified after the fix. Claude and the local clients ship regardless.

## Text fallback

- Tool results keep their text: `content[]` stays byte-identical for results without the one-time update hint, and `structuredContent` only gains result-only fields (`lang`, `ask_ends_at`, a `ready` flag; Design 4). Stored plans and their schema don't change. A golden test compares hint-free results with and without the card; the hint is tested on its own.
- The card link lives in the tool's `_meta`, which `TestMCPToolTextStable` doesn't hash (`cli/mcp_plain_test.go:13-39`), so the shipped text of existing tools stays. The new `ha_status` changes that hash on purpose (contract call-out in the PR); claude.ai keys "Always allow" per tool, so only the new tool needs one. The tools version (`mcpToolsVersion`) changes, which fires ChatGPT's one-time Refresh hint; that is correct.
- A host that doesn't render, a declined "Allow", a card that fails to load, or a failed `resources/read`: the chat runs on today's text, including the assistant's check-back after a phone wait. Nothing falls back to a weaker confirmation, and a card error never turns into "confirm with yes".
- The card shows nothing the text lacks, except the live status after the assistant's answer ended.

## Design

1. **`ha_status`, a new read-only tool** ("Check a change", Use the home and above, in `chatToolLevel` and among the tools that run beside a change). Given a plan id of the caller's own, it answers how the change stands: waiting for the phone (with `ask_ends_at`), applying, the outcome, or "unknown". It never starts an apply or an ask, never sends a code, never takes a job's outcome away from `ha_apply`, and never carries the one-time update hint (a card's call would use it up unseen). An App without it refuses the unknown tool, so a cached card talking to an older App fails closed. The assistant may use it too, instead of calling `ha_apply` again; `ha_apply` itself stays as it is.
2. **Keep the finished outcome for `ha_status`.** Today a finished chat apply hands its outcome to one caller and deletes the job (`cli/mcp_serve_apply.go:20, :280`). Keep a read-only copy of the final outcome (done, verified, declined, cancelled, refused, partly done, or may have run) per Home Assistant user and exact plan id until `planTTL`, checked when it is read. Errors a person can correct (`CODE_REQUIRED`, `CODE_MISMATCH`, `BUSY`, a snapshot failure) aren't kept, so a corrected code or a retry still works through `ha_apply`. `ha_status` re-checks the user's level and the read-only switch on every call, as `ha_apply` does after a tap (`cli/mcp_serve_phone.go:338-373`). Outcomes it can't prove ("may have run", access changed while it ran) stay exactly that, never "done".
3. **The view.** `cli/mcpapp/view.html`, vanilla HTML/CSS/JS, embedded with `go:embed` like the setup page, about 300-500 lines and under 50 KB, with a small hand-written message bridge (handshake, tool call, message, size change, host theme). The official MCP Apps SDK was checked and stays the fallback if a host needs it. Served as `ui://ha-nova/view-<8 hex of its hash>.html` with MIME `text/html;profile=mcp-app`, so a new file gets a new address.
4. **What the card shows comes from fields the server sets, never from prose.** Tier, channel, the `ready` flag (false while a raw plan still says not to show its preview, for example before the consumer check), expiry, rows, consumers, coverage notes, and `ask_ends_at`. The card shows a button only for a plan the server marks ready and in the chat tier; any unknown shape shows one line, "Details are in the reply", and no button. Outcome states map one to one: done, verified, accepted, no change, declined, cancelled, refused, partly done (with its ledger), and may have run (`PLAN_NOT_FOUND`, `PLAN_CLAIMED`, unknown outcome) never shown as done or "nothing changed".
5. **Following a phone wait.** After the person's click starts the ask, the card calls `ha_status` with one call in flight at a time, at most every 10 s, until an outcome or `ask_ends_at` (the server's own end, never a timer started when the card appears), and stops on teardown. Its lines keep three states apart: "Watch your phone", "Still checking", and "Can't tell yet; ask the assistant".
6. **Linking.** `mcpToolList` adds `_meta.ui.resourceUri` (plus the older flat `ui/resourceUri`) to `ha_apply` and `ha_plan`; `readResource` answers the `ui://` address before the skills. One list and one resource serve the local and the chat server. `lang` goes into the result so labels match the language of `say` (fallback: the host's locale, then English).
7. **Docs only, no Relay copy.** `chat-apps.md`: one line under "Stop Claude from asking" ("When Claude asks to show HA NOVA's cards, choose Always allow"), and the phone section says the card shows the outcome where it appears. The "How Anna joins" box on the NOVA page stays as it is. Visual work follows the impeccable skill with before/after screenshots.

Review at build time: two adversarial lenses (fail-open; UX and agent guidance) over the final diff, an end-to-end agent run on the test bed, and the Codex round.

## Phases and effort

| Phase | What | Effort | Lands |
|---|---|---|---|
| 0. Prototype | A stub card in Claude Desktop with the local `ha-nova mcp` against the test bed only. Measure frame height and scroll jump, whether "Allow" comes per tool or per connector, whether calls from the card prompt or time out, and whether stacked frames bother. The remote path (Claude on the web and a phone over a public address) runs on a throwaway Home Assistant and goes to Markus as a handover, since the accounts and phones are his. Never his home. | S | before 1b |
| 1a. Status tool | Design 1-2 with tests: no start, no code, no hint, no taking an outcome; corrected code and retry still work; level lowered or user removed; two plans for one user; another user's plan id; expiry checked on read; an older App refuses it. | S-M, ~80 Go lines | 0.37 |
| 1b. Status card (F1) | Design 3, 5, 6 on `ha_apply`, catalog labels, docs. Tests: resource and MIME, both `_meta` forms, golden hint-free text, the same resource for two users, the `textContent`-only check, tools version changes. | M, ~100 Go lines plus the view | 0.37, with the train's one App bump |
| 1c. Plan card (F2, F5) | Design 4 on `ha_plan`: the `ready` flag and rows. | M | 0.37 if 1b is in time, else 0.38 |
| 2. Rename table (F3) | Old and new name and id per row in the raw plan result (today it has a sentence, endpoint, and tier). ChatGPT check after MAR-150. | M-L | 0.38 |
| Later | When hosts say per request that they render cards (MCP revision 2026-07-28), the assistant's phone-wait answer can get shorter, and D3 can be revisited. | — | — |

## Risks

- **Host differences.** Heights, timeouts, and the "Allow" prompt are partly undocumented. Phase 0 measures them; the text path covers any gap.
- **Stacked frames.** While a person waits, the assistant may still call `ha_apply` again in the same answer, and each result may show a frame. Each stays one line. Accepted for v1.
- **The first prompt comes at a tense moment.** With F1 first, a household member's first card is likely their first unlock wait, so Claude's "Allow" comes right then. The doc line covers it; declining keeps the text path.
- **Markup in device names.** Covered by the `textContent` rule and its test.
- **Our own message bridge.** We own a small bridge instead of a library; the SDK stays the fallback.
- **ChatGPT keeps old cards.** Each new file fires the Refresh hint again, so card changes land at most once per train.

## Open decisions for Markus

- **D1. #978 in chat apps: what to offer?** The full sweep is computer-only, because bringing it into chat needs a new `ha_get` argument, which resets every claude.ai user's "Always allow" for `ha_get` once. The chat has a shorter static check today, as text. Options: A) no table; the person names the fixes, and F2 makes confirming them one tap; B) bring the full sweep into chat later with a pick table on its result only (one reset, and `ha_get` linked to the card). **Recommendation: A for 0.37; B as a 0.38 candidate if people ask for it.**
- **D2. A chat code that only the card shows?** The AI sees the code today, which is why codes protect less than the phone and stay at Manage everything. A host that can show cards may still not show one (declined, failed), so the server can never know the person saw a code that only the card has. Hiding it from the text would break Manage-everything setups without a phone that work today. **Recommendation: no; keep today's codes and the "Needs approval" advice. The real fix for people without a phone stays linking one.**
- **D3. Link `ha_call` too, for "Go ahead" on Use-the-home changes that ask for an OK?** It saves one typed "yes" but may show a frame next to every "light on". **Recommendation: not in 0.37; decide after phase 0 shows what a one-line frame costs on a phone.**
- **D4. A code field in the card?** The person would type the code into the card instead of the chat. It adds no safety (the AI sees the code) and saves little; a button that fills or sends the code itself is out, because it would make a click the confirmation. **Recommendation: no field in 0.37.**

Kept as today, not decisions: the phone for unlocks, doors, gates, garage doors, the alarm, and deletes; codes only at Manage everything; the separate OK for updating automations after a rename (F3 makes it one tap).
