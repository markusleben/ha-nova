# MCP: the human types the code, not the model — Spec (2026-10-16)

Status: active

## Problem

Over MCP the confirmation code of a typed plan (locks, gates, garage doors,
alarm disarm, deletes) is in the `ha_plan`/`ha_call` result, so the model can
pass it to `ha_apply` itself. The only human gate is the client's
tool-approval prompt (`docs/reference/client-integration.md`).

## Behavior

1. **Capability.** Form elicitation is available when the client's
   `elicitation` capability is an object that names `form`, or names neither
   `form` nor `url` (`{}` means form in 2025-06-18 and later). Legacy era: read
   once from `initialize.capabilities`. 2026-07-28: read per request from
   `_meta.io.modelcontextprotocol/clientCapabilities`. `{url:{}}` alone is no
   form support.
2. **Code withheld.** In every tool result of a request with form
   elicitation, `plan.code` is removed and the typed ask in `say` becomes
   "Your app will ask you to type a code to go ahead — a plain yes can't
   confirm this." `next_step` tells the model to call `ha_apply` without
   `confirm_code` when the user asked for the change, and never to ask for the
   code in chat. The CLI output and clients without forms are unchanged.
3. **`ha_apply` asks the human.** With form elicitation, `confirm_code` is
   always dropped (a model-supplied code is never used). When the stored plan
   is typed — readable, not expired, of the pinned server, a call that froze a
   target, `no_snapshot` only after a failed snapshot, with confirmation text —
   the server asks the client for a form:
   - message: the plan's code-free confirmation text (stored with the plan at
     plan time; a config plan names its live item first: "Delete the
     automation “Night lights”.", "Change …"/"Create …"), the copy note for a
     delete ("A copy is saved first…", or that none could be saved with
     `no_snapshot`), then "To go ahead, type this confirmation code: <6 hex>"
     — the six characters after the twelve the plan id shows, i.e. the code's
     own suffix;
   - `requestedSchema`: one required string `code` titled "Confirmation code"
     (6 characters). No `mode` field (form is the default in every revision).
   Otherwise the plan goes to apply without a code, which refuses it as the
   CLI does.
4. **Transport per era.**
   - Legacy (`initialize`): the server sends `elicitation/create` inside the
     `tools/call` and waits for the response (at most 5 minutes, never past the
     plan's expiry). Meanwhile it answers `ping`, queues every other message,
     and stops without a reply when the client cancels that `tools/call` or
     closes stdin; on timeout it cancels its own request
     (`notifications/cancelled`), also when the client cancels the call. A
     waiting request the client cancels never runs. 2024-11-05 has no
     elicitation: no forms there.
   - 2026-07-28 (MRTR): `ha_apply` returns `resultType: "input_required"` with
     `inputRequests.confirm` (`confirm_without_copy` for a `no_snapshot`
     delete, so an answer to one form never confirms the other); the client's
     retry carries the answer in `inputResponses`. A malformed answer is
     `-32602`. No `requestState`: the code binds the answer to the plan.
5. **Outcomes.** `accept` with six hex characters → apply with `<code
   prefix><typed suffix>` (trimmed, lowercased), once; apply's checks are unchanged: claim,
   recompute, drift, code from the recomputed plan, one send, three wrong
   codes discard the plan. `decline` → `CONFIRM_DECLINED`; `cancel`, an answer
   that is not six hex characters (no attempt used), or a timeout →
   `CONFIRM_CANCELLED`; a JSON-RPC error from the client →
   `CONFIRM_UNAVAILABLE`. All three: exit 4, nothing sent, the plan untouched
   (no claim before the answer); their wording never claims the user saw a
   form. A mistyped code is `CODE_MISMATCH` with a "show the form again" step;
   `error.next` always says what `next_step` says.
6. **Preview tier: no form.** Preview plans (expanded targets, scenes,
   scripts, config writes, restart) keep the natural yes in chat: their
   consequence is bounded (everything access-related or irreversible is typed
   by construction), the write card's "show yaml" option has no form
   equivalent, and a form after the client's own approval prompt would put
   two prompts on everyday batch actions.

## Spec conformance (MCP 2025-06-18, 2025-11-25, 2026-07-28)

- Form mode must not request sensitive information — secrets or credentials
  that grant access or authorize transactions. The code is neither: the
  server shows it in the same form, it means nothing outside that plan, and it
  is single-use with a 15-minute expiry. Its only job is a deliberate action
  the model cannot perform.
- `accept`/`decline`/`cancel` are handled distinctly; servers must not assume
  success; the server sends only modes the client declared; MRTR retries
  without the answer get a new `InputRequiredResult`.
- Wire shapes cross-checked against `modelcontextprotocol/go-sdk` v1.8.0
  (`ElicitParams`, `ElicitResult`, `CallToolParams.InputResponses`); the
  adapter stays stdlib-only.

## Clients (checked 2026-10-16)

| Client | Evidence | Path here |
|---|---|---|
| Claude Code CLI 2.1.301 | changelog: form + URL elicitation since 2.1.76, stdio servers on 2026-07-28 since 2.1.292; local probe: `server/discover` with `elicitation:{form:{},url:{}}` | form (MRTR) |
| Codex CLI 0.162.1 | local probe: `initialize` 2025-06-18 with `elicitation:{form:{},url:{}}`; the TUI renders form fields | form (legacy) |
| VS Code 1.102+ | release notes 1.102: elicitation support; declares `{form, url}` (apify client index) | form (era not probed) |
| Cursor | its MCP docs list elicitation as supported; declares `{}` (apify client index) | form (era not probed) |
| Claude Desktop (local stdio) | its MCP logs here (2026-10-15): 2025-11-25, no `elicitation` | unchanged: code in result |

## Known limits

- A client that declares form elicitation but answers without showing a form
  (headless `claude -p`/`codex exec`, auto-approve modes, some extension
  surfaces — reported for Claude Code's VS Code extension, anthropics/claude-code#98256)
  cannot confirm a typed plan, and its decline looks like a person's: nothing
  is sent, and the result points to Home Assistant or the CLI. A
  `--no-forms` opt-out is the follow-up if testers need one.
- Client-side automation that answers forms (for example a Claude Code
  `Elicitation` hook) answers for the user; that is the user's configuration.
- The form text is English (the CLI has no translations); the agent's preview
  right before it is in the user's language, and the code is language-neutral.
- The plan file (0600, the user's cache) holds the form text and the nonce:
  whoever can write it, or runs a shell, can run `ha-nova apply` directly. The
  form gates the MCP path, not a shell.
- Legacy era: while a form is open, other requests wait (up to 5 minutes).

## Verification

Go tests (`cli/mcp_confirm_test.go`): code withheld (plan, call, drift, apply,
snapshot-failure results); apply elicits in both eras; mismatch, decline,
cancel, malformed or empty code, timeout, client error, a cancelled
`tools/call`, a cancelled waiting request, and stdin closing never send; the
model's `confirm_code` is ignored; one send, and a used plan refuses a
replayed answer (a failure before any send keeps the plan, like the CLI);
clients without forms unchanged.
