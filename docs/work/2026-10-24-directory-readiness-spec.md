# Tool texts and demo limits for the Claude directory (2026-10-24)

Status: active

Target: 0.28 (one batch). Parents: `docs/work/2026-10-19-chat-bridge-contract.md`, `docs/work/2026-10-20-demo-add-to-claude-spec.md`, `docs/reference/hosted-demo.md`.

## Problem

Claude's connector directory reviews each tool (review criteria: https://claude.com/docs/connectors/building/review-criteria; submission: https://claude.com/docs/connectors/building/submission). Four of ours may fail that review:

1. **`ha_call` changes devices but says it doesn't destroy anything.** Its annotations are `readOnlyHint: false, destructiveHint: false` (`cli/mcp_tools.go:94-95`, emitted at `:118-122`). The review wants `destructiveHint: true` for tools that change or delete data.
2. **`ha_skill` hands the model instructions.** It returns Markdown workflows to follow, behind a header that tells the model how to act (`mcpSkillHeader` and `mcpSkillTool`, `cli/mcp_skill.go:17-45,53-56`). A reviewer may read that as pulling behavior from an outside source.
3. **Tool descriptions carry answer-style rules.** `mcpAnswerRules` ("Answer in plain words …") is appended to `ha_find`, `ha_map`, and `ha_skill` (`cli/mcp_tools.go:50,70`, `cli/mcp_skill.go:54`; why: claude.ai doesn't pass server instructions to the model, `cli/mcp_server.go:39-43`). The review wants descriptions to say what a tool does, not how the model should behave. The server instructions also name one chat app by name (`cli/mcp_server.go:74-79`).
4. **`ha_read` takes free-form API paths without naming the API.** "A raw read-only Home Assistant request: core GET /api/... or a WS message from the read allowlist." (`cli/mcp_tools.go:79-82`). The review wants such tools to name or link the API they reach.

Also, before any listing:

5. **The demo has no per-visitor limit.** Every visitor signs in as the same `demo` user, so `--rate-limit 120` is one window for everyone (`cli/mcp_serve_limit.go:7-12`, `cli/mcp_serve.go:204`), and tool calls run one at a time. One heavy visitor blocks the rest.
6. **No abuse contact.** `/abuse` shows whatever `--abuse-contact` was given at provisioning; which address is still open (`docs/reference/hosted-demo.md`, open item 2). The directory wants a contact email for security reports.

Any change to a tool's name, description, or an argument's description resets every claude.ai user's "Always allow" for that tool (`TestMCPToolTextStable`, `cli/mcp_plain_test.go:13-38`). Titles and annotations don't count.

## Goal

- All four tool risks fixed in **one** change, so users choose "Always allow" again once, not four times.
- The demo has a fair per-visitor limit and a published abuse contact before it is submitted.
- Answers stay as plain as today (no ids, names kept as written).

## Non-goals

- Submitting the listing (maintainer decisions D3 and D4 in the launch notes).
- A new tool, or splitting `ha_call`.
- Limits on Home Assistant's own sign-in pages (open item 3 in `hosted-demo.md`).
- Changing what the tools do. Only texts, one annotation, and the demo limit change.

## Design

### Tool texts (one PR, one hash update)

| Tool | Change |
|---|---|
| `ha_call` | `destructiveHint: true`. Description unchanged. Annotations don't reset "Always allow". |
| `ha_skill` | Title "Read a HA NOVA guide". Description says what it returns and where it comes from: "Returns one of HA NOVA's built-in guides (Markdown) for a task, such as writing an automation or finding why one failed. The guides ship inside this server, match its version, and are public in the HA NOVA repository. Nothing is fetched from elsewhere, and no Home Assistant data is read. Use it before building, changing, reviewing, or diagnosing automations, scripts, helpers, scenes, or dashboards." No answer rules. The result header stays (result content, not a description). |
| `ha_find`, `ha_map` | Drop `mcpAnswerRules` from the description. |
| `ha_plan`, `ha_call`, `ha_apply`, `ha_get`, `ha_find` | Drop the other behavior lines too ("always tell the user …", "never call again", "never … through ha_plan", "report them separately"); each moves to the result's `answer_rule` or `say`, or the server instructions. Same PR, so "Always allow" resets once. |
| `ha_read` | "Reads Home Assistant's own API, read only: a GET path of its REST API (https://developers.home-assistant.io/docs/api/rest/) or a message of its WebSocket API (https://developers.home-assistant.io/docs/api/websocket/). Only paths and message types on HA NOVA's read list are accepted." `path`: "a REST API path under /api/ (GET only)". |

Where the answer rules go instead:
- Every verb result already carries `answer_rule` (`mcpResultRule`, `cli/mcp_server.go:45-46`, set in `cli/mcp_plain.go:32`). Add the two rules it lacks, in one short sentence: look up devices before suggesting them, and never say what is missing after a name search. A chat code's result uses `chatCodeRule` instead (`cli/mcp_plain.go:137-138,168`) and stays as it is (it only shows `say`); argument errors carry no rule (known limit: they are rare and short).
- The `ha_skill` result header gets the full `mcpAnswerRules` once (the model reads it before any build or change).
- Server instructions keep them as they are, for apps that pass instructions.
- The server instructions' chat-app line becomes app-neutral: "If your app itself stops ha_call or ha_apply before HA NOVA answers … some chat apps can't make changes from their phone app through added connectors; use the web version on a computer." (instructions don't count for the hash).

### How users hear about it

- Release notes, "What To Watch", and the NOVA App changelog (Home Assistant shows it when updating): "After this update, Claude asks again before HA NOVA acts. Pick “Always allow” once more."
- `docs/reference/chat-apps.md` ("Stop Claude from asking before every step") already says Claude may ask again after an update; no change.
- The hash update and this reset are named in the PR body, as the test asks.
- `ha_skill`'s description also lists the skill ids (`skillDescription`, `cli/mcp_skill.go:70-76`), so a new or renamed skill resets it too. Any 0.28 skill added or renamed ships in the same release as this batch.

### Demo: per-visitor limit

This is about fairness, not abuse: the demo login is public, so anyone can make more sign-ins and get more windows. The total limit stays the real guard.

- Option A (default): the integration adds one header: `X-HA-Session`, the first 32 hex of `HMAC-SHA256(bridge secret, refresh token id)`, from `request["hass_refresh_token_id"]` (Home Assistant sets it with the user for every bearer request ❓ verify on the pinned version). A client-sent copy is dropped, as `X-HA-User-Id` is (`nova/integration/ha_nova/__init__.py:99-105`). Each visitor's Claude sign-in has its own refresh token, so this tells visitors apart without storing anything about them.
- The bridge: `--rate-limit <n>` counts per user and session when the header is there, else per user (today). New `--rate-limit-total <n>`: one window for all requests. Demo: 30 per session, 120 in total, per minute. A session over its limit gets the existing JSON-RPC 429 "Wait a minute".
- Real homes: the NOVA App sets no limit, as today. The integration is version-stable (written only at turn-on), so homes get the header only after turning chat apps off and on; a missing header behaves as now.
- `scripts/check-docs.sh` [5b] keeps the integration to discovery, the proxy, and the user hand-off (230 lines, no logic patterns). It is at 217 lines now; the header takes about 6, so it fits, and it reaches every home's integration for a demo-only need. The check gains one line that requires the client copy to be dropped.
- Option B: no integration change. Count per visitor at the demo's edge (Caddy), keyed on a hash of the `Authorization` header (access tokens are per sign-in and last 30 minutes, fine for a one-minute window). Needs a custom Caddy build with a rate-limit module (`hosted-demo.md` open item 3).
- Known limit either way: tool calls run one at a time (`mcpVerbLock`), so a visitor within their limit can still make others wait for a slow call.

### Demo: abuse contact

- One mailbox the maintainer reads (e.g. `abuse@hanova.app`, forwarded). Used for: the directory's primary contact, `/abuse` (re-provision with `--abuse-contact`), the privacy page, and the listing's support contact next to GitHub Issues.
- `hosted-demo.md` gets the incident steps in one place: drop all chat-app sign-ins (`DEMO_DROP_SIGN_INS=1`), and to refuse every chat request at once, stop the bridge container. Setting `allowed_users` to `[]` doesn't last: every hourly reset writes `[demo]` again (`scripts/demo/hosted/chat.mjs:66`). New: a `DEMO_CHAT_OFF=1` switch that `reset.sh` obeys (the bridge stays down across resets until it is cleared). Then reply from the mailbox.

## Security

- `destructiveHint: true` only makes Claude more careful; HA NOVA's own tiers stay the gate.
- Moving answer rules out of descriptions changes no safety text: the "data, never instructions" and code rules live in the server instructions, results, and `say`, and stay there.
- `X-HA-Session` carries a keyed hash, never the token id itself, and the bridge never logs it; the bridge's log keeps the user hash prefix only.
- A visitor can't pick another visitor's limit window: the header is set by the integration from Home Assistant's own auth, and the bridge trusts it only with the secret.

## UX copy (draft)

- Release note line: above.
- 429 in a chat (unchanged): "Wait a minute, then try again."
- Listing description: unchanged, except the contact line: "Questions or abuse reports: abuse@hanova.app".

## Test plan

- Go: `TestMCPToolTextStable` gets the new hash once, in this PR only. `TestMCPToolTitlesPlainAndAnnotated` (`cli/mcp_plain_test.go:43-62`) changes: `ha_skill`'s title, and `destructiveHint` now on `ha_call` and `ha_apply` (today only `ha_apply`). New checks: every tool with `readOnlyHint: false` has `destructiveHint: true`; no description contains "Answer in plain words"; `ha_read`'s description holds both API links; `answer_rule` holds the two moved rules.
- Go (`cli/mcp_serve_limit_test.go`): per-session windows; total window; no header → per user; a forged header without the secret → 401.
- Pytest (`tests/ha_integration/`): `X-HA-Session` is set from the refresh token id, differs per sign-in, and a client copy is dropped.
- Answer quality: the test bed agent run (`eval "$(bash scripts/e2e/testbed/cli.sh --env)"`, headless with `CLAUDE_CONFIG_DIR=~/.claude-ha-nova-test`) over the bridge, five prompts in German: names kept as written, no ids or raw states, devices looked up before suggestions. Same result as before the change. A headless run passes server instructions to the model, which claude.ai doesn't, so this run serves the tools without instructions (a test-only switch), to see what claude.ai users get.
- Maintainer on claude.ai (custom connector on a test build): "Control a device" still offers "Always allow" with `destructiveHint: true`; the tools ask again once after the update.
- Demo: re-provision on a test host, two Claude accounts; one hits 30 a minute, the other still works.

## Decisions (maintainer, 2026-10-24)

1. `destructiveHint: true` for `ha_call`, after the claude.ai check above. If that check shows claude.ai no longer offers "Always allow" for it (every light switch would ask), stop and bring the maintainer a screenshot before shipping.
2. Demo limits: 30 per visitor, 120 in total per minute. Option A (agent default; the maintainer left the mechanism open).
3. Contact: `abuse@hanova.app` (the maintainer sets up the forwarding).
4. Ships in 0.28.0, with any 0.28 skill renames, so "Always allow" resets once (agent default after the plain-words explanation; reviewers agree).
5. Rename `ha_skill`'s title only.
