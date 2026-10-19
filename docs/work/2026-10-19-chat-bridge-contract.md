# Chat-app bridge contract: `ha-nova serve` (2026-10-19)

Status: active

Parent spec: `docs/work/2026-10-18-onboarding-0.27-spec.md` §11. Scope: the contract between `ha-nova serve` (the bridge, PR train B1), the NOVA App that runs it and writes its files (B2), and the Home Assistant integration that forwards to it (B3). Behaviour beyond this contract is the code in `cli/mcp_serve.go` and `cli/mcp_serve_phone.go`.

## Path

Chat app → Home Assistant OAuth → integration view `/api/ha_nova/mcp` (pure pass-through) → `ha-nova serve` on an internal port of the App container → Relay → Home Assistant.

## Command

`ha-nova serve --listen <host:port> --data-dir <dir> --secret-file <file> [--confirm-wait 25s] [--server <name>]`

- `--listen`: an explicit host is required (the App passes `0.0.0.0:<port>`, never mapped to the host).
- `--secret-file`: exactly 64 lowercase hex characters (256 bits), mode `0600`. Read once at start.
- `--confirm-wait`: 1 s to 2 min; how long one `ha_apply` waits for a phone tap.

## HTTP

- `POST /mcp`, one JSON-RPC message per request, one JSON answer (`application/json`). Notifications and responses get `202` with no body. `GET` and `DELETE` get `405`. No SSE, no sessions (`Mcp-Session-Id` is accepted and ignored; none is issued), no elicitation.
- `Mcp-Protocol-Version`: a legacy revision (`2025-11-25`, `2025-06-18`, `2024-11-05`) selects that era; absent means `2025-11-25`; `2026-07-28` requests carry `_meta` per request; any other value gets `400`.
- Body limit 1 MiB.

## Authentication (every request)

1. Exactly one `Authorization: Bearer <secret>`, compared in constant time.
2. Exactly one `X-HA-User-Id`: the Home Assistant user the integration authenticated (`^[0-9a-f]{32}$`).
3. That user is allowed (below).

Refusals: 1–2 → `401`, 3 → `403`, unreadable settings or a failed owner read → `503`. The integration must set both headers itself, from its own secret and `request["hass_user"]`, and must drop any client-sent copies. The user header is trusted only together with the secret.

## Files in `--data-dir` (written by the App)

- `chat-apps.json`, re-read on every request. Unknown fields or trailing data refuse every request.
  - `allowed_users`: list of HA user ids. Absent means the HA owner only; an empty list allows nobody.
  - `read_only` (default `false`): refuses `ha_call`, `ha_plan`, and `ha_apply`.
  - `chat_codes` (default `true`): with `false`, a typed plan needs the user's linked phone.
- `phone-links.json`: `{"<HA user id>": {"service", "device_id", "user_id", "name"}}`, the bridge's only link store (never a computer's `config.json`). A link's `user_id` must equal its key; `null` or a malformed entry is an unusable link (refused, never "no phone"). An unreadable file refuses typed plans.
- Write both files atomically (temporary file plus rename): a missing `chat-apps.json` means the defaults.
- `users/<hash>/`: owned by the bridge (per-user plans).

## Tools and confirmations

- The same 10 tools, server instructions, and verbs as `ha-nova mcp`. Plans and pending phone asks are bound to the HA user that made them; another user's plan id (in `ha_apply` and in a `waiting_for_phone` re-call) answers exactly like an unknown plan.
- Typed plans: the linked phone of the requesting HA user (an active administrator, as in the CLI), else a chat code with the fixed low-protection note, unless `chat_codes` is `false`. Never a window or a form.
- `ha_apply` on a phone plan waits up to `--confirm-wait` (default 25 s pending the Home Assistant Link timeout test; spec §11.4 names 45 s), then returns `status: waiting_for_phone`. Calling again joins the same ask (no new notification). A tap applies at once; the owner's switches are re-checked before it applies. One open phone ask per user (`CONFIRM_PENDING` otherwise). An ask stays open 3 min or until the plan expires.
- The integration's own timeout must exceed `--confirm-wait` plus about 10 s, and must map its timeout to a readable error.
