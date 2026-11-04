# Home Assistant Link route: its own budget, one bounded retry, a remembered route

Status: active

Date: 2026-11-03. Owner: CLI transport (`cli/relay.go` health and `relay ws|core`, `cli/cloud_runtime.go`, `cli/cloud_ingress.go`, `cli/cloud_link_budget.go`, `cli/relay_session.go`, `cli/trace_loader.go`). Target: 0.28.1. Changes the routing rules of `docs/work/2026-07-25-home-assistant-cloud-remote-spec.md` (§Routing, Non-goals) and `docs/reference/safety.md` (a service call is sent once).

## Today

Symptom (maintainer's Mac): `route_policy: automatic`, Home Assistant Link ready. With a VPN on, the Mac reaches the App on its LAN address and everything works. With the VPN off, no confirmation goes through.

Measured on 2026-11-03 with read-only probes, VPN on:

- Network to the Link origin: DNS 6–42 ms, TCP ~30 ms, TLS ~70 ms, first byte ~55 ms (0.15–0.29 s in total), also after 30 s idle.
- `relay health --via cloud` with a 30 s budget: 1.2–3.7 s, once 6.9 s. Seven runs with the default budget: two failed at 5.2–5.3 s with `cloud_unavailable`.
- Phases, from connection open and close times: startup and secure-storage reads 0.13–1.6 s; OAuth refresh 0.13–0.7 s, 1.1–1.9 s after idle; WebSocket dial, auth, and five calls 0.6–1.1 s; the Relay request through Ingress 0.2–1.0 s.
- An address with no route (192.0.2.1, standing in for the App's LAN address without the VPN) waits for the full timeout, so the automatic local preflight costs its full 2 s.

## Root cause

1. `relay health` puts the whole transport selection under one `--connect-timeout` context (5 s by default; 0.5 s in `hooks/session-start`). Selection is not one connection. It covers the secure-storage reads, the 2 s automatic local preflight, the OAuth refresh, the WebSocket dial and auth, five WebSocket calls, and the Ingress session. When it runs out, the user sees "Home Assistant Link is temporarily unavailable (code cloud_unavailable; next: retry_later)". With the VPN off, the preflight uses 2 of the 5 s, so health fails whenever the Link login takes more than about 3 s. The session-start probe fails every time. The skills' `ha-nova relay health` preflight fails too, and the agent sends the user to `ha-nova setup` before it ever runs `apply`.
2. Nothing on the Link route is retried, and nothing is remembered. Every CLI process probes the LAN address again (2 s) and logs in to Link again. A window or phone confirmation logs in three times in two processes: `call`/`plan`, the session that asks, and the session that applies after the answer. One slow or dropped step anywhere fails that command. If it is the ask session, apply answers `CONFIRM_IN_WINDOW`. If it is the apply session after the human confirmed, the confirmation is lost and nothing is sent.

## Design

### Budgets

- A connection to the Link origin (TCP and TLS) has 5 s (`linkConnectTimeout`), about 25 times the measured worst case. The Link HTTP client keeps up to 8 idle connections, one per concurrent session read.
- `relay health`: `--max-time` bounds the selection and the request. `--connect-timeout` bounds each connection the command opens (Link and local), as the 2026-07-25 spec says, including the automatic local preflight (the smaller of 2 s and the flag).
- One Link login attempt has at most 10 s (`linkLoginAttemptTimeout`), and always ends by the caller's deadline: health `--max-time`, a verb session's 30 s, `relay ws|core` 15 s.
- `relay ws|core`: the request gets its own 15 s after the route is chosen, so a 10 s `collect_events` window still fits after a slow login.
- `trace`: one route selection per command, not one per Relay call (three for `trace latest`).
- The Home Assistant WebSocket closes without the close handshake, which waits up to 10 s for a silent peer, past every caller's deadline.

### Retries: exactly these, each at most once

| # | What | When | Why it is safe |
|---|------|------|----------------|
| R1 | The Link login: OAuth refresh, WebSocket checks, Ingress session | A network or timeout error without an HTTP answer, or a server-side 5xx; at least 3 s left. A refused WebSocket upgrade (400, 403, 429) is final | No functional request exists yet. A refresh grant only mints a short-lived access token, the WebSocket calls read, and an unused Ingress session expires on its own. |
| R2 | Any Link request whose connection never opened | DNS, TCP, or TLS failed and no connection was obtained (`GotConn` never fired); at least 1 s left | Nothing was written, so nothing reached the Relay. This holds for writes too. |
| R3 | A Link GET (`/health`, setup's info reads) | No answer, Ingress unavailable, or a 502–504 that did not come from the Relay; at least 1 s left | A GET only reads. |
| R4 | A session read over Link or, since 0.28.1, on the local route (`relaySession.sendIn`: the read allowlist, the phone lookup and listening probes, verify polls) | No answer from the Relay: no HTTP answer, Ingress unavailable, or a status Link or Home Assistant answered instead of the Relay; inside the same per-request deadline, at least 1 s left | Every caller of `sendIn` reads. Writes go through `sendWrite`. |

Never retried:

- a write (service call, config save or delete, dashboard or area write, snapshot save, phone notification) once its connection opened;
- anything that got an answer from the Relay;
- any authentication, identity (Relay instance, Home Assistant user), redirect, protocol, subscription, or secure-storage failure;
- anything else on the local route (unchanged). R4 covers local session reads since 0.28.1, but only a connection that was refused, reset, or closed before the Relay answered: a port forward with a small accept backlog (a VM's NAT) refused some of the read-ahead's parallel connections, and each refused read made an ordinary call ask. A local timeout or any TLS failure (an alert, a bad certificate, a wrong pin) stays final.

A TLS verification failure counts as a connection that never opened, so R1–R3 may repeat it once; every attempt verifies before anything is sent.

R2 can sit under R3 or R4, but an R2 attempt writes nothing: a session read reaches the Relay at most twice, a login's reads at most four times (R1 around R3), and a write at most once.

This narrows the 2026-07-25 non-goal "No automatic retry after a functional request may have reached the Relay". A read on the allowlist (R3, R4) may be sent once more; a write never.

### Remembered route (automatic only)

- `<cache>/link-route.json` records the profile id, the Relay instance id, the Relay's local address, and an end time at most 2 minutes ahead. Uninstall removes it.
- It is written only when the full 2 s authenticated local preflight (not shortened by `--connect-timeout`) failed with a pure network error without ever obtaining a connection, and the Link login that followed succeeded with its identity checks. A slow but reachable local Relay is never replaced.
- It is read only by `route_policy: automatic` for the same profile, instance, and address, and only while Link is open (no cleanup checkpoint). It skips only the local preflight. The device-credential read and the full Link identity check still run; during cleanup the local Relay is probed as before.
- A hit never extends it. When Link fails on a remembered route and the caller still has time, the file is removed and the local Relay is probed in the same command, before any functional request exists: a healthy local Relay is used, otherwise the Link error stands. When the caller's own deadline ends the login, the route stays.
- Commands within two minutes of the probe skip it; an answer that comes later than that pays it again.

## Invariants

- No duplicate write: R2 is the only retry that can carry a write, and only when no connection was obtained.
- No weaker confirmation: plans, seals, channels, codes, and the window and phone flows are unchanged; the retries sit below them.
- No route to a wrong instance: every Link login still checks the Relay instance and the Home Assistant user. The remembered route is keyed to the instance and can only choose Link.
- Fail closed: a pin, TLS, authentication, or protocol failure of the local preflight never writes the remembered route and still never falls back.

## Not changed

- `nova/` and the chat bridge: the bridge runs inside the App and reaches Home Assistant through its own local connection, never through Link.
- `hooks/session-start` keeps `--connect-timeout 0.5 --max-time 2`. A cold Link login needs more than 2 s, so with the local Relay unreachable the hook can still warn. Follow-up.
- Each session logs in to Link on its own, except apply after a window or phone Confirm (Follow-up below).
- Doctor and setup's device check also use a remembered route for up to two minutes.
- Through an HTTPS proxy the TLS handshake keeps a fixed 5 s (`--max-time` still bounds it). Setup and cleanup calls to Link get the same 5 s connect limit.

## Tests (each fails on 8cf3bb1a)

- Health: the selection deadline is `--max-time`, and the connect budget reaches the resolver.
- Slow Link connect: a connection that does not open in time fails at the connect budget, not at the transport default.
- Login fails once (network), then succeeds: one command, two logins, success. Authentication and identity failures are not retried.
- Local unreachable, then Link: a second automatic selection within 2 minutes skips the preflight; another Relay instance or a far end time is probed again. When Link fails on a remembered route, the local Relay is probed in the same command and used when it is back.
- `relay ws|core` gets its full request time after a slow route choice; `trace latest` chooses its route once.
- A write whose request was sent is never retried. A write whose connection never opened is sent once. A session read goes once more only without an answer from the Relay; on the local route only after a refused, reset, or dropped connection, never after a timeout or a TLS failure.
- A refused WebSocket upgrade is not retried; a 504 from Link counts as no answer; the WebSocket closes without waiting for a silent peer; uninstall lists the route file.
- Guards that pass on base too: a security failure, a shortened probe, or a probe that connected remembers nothing; the caller's own deadline keeps the route; cleanup still uses a healthy local Relay; a rejected grant is final.

## Follow-up: a Confirm hands its route to apply

Date: 2026-11-03. Target: 0.28.1, follow-up to #944. Owner: `cli/confirm_handoff.go`, `cli/verb_apply.go`, `cli/dashboard_apply.go`.

### Hand-off

- After a Confirm in the window, on the phone, or in the window while the phone asks too (CLI and `ha-nova mcp`), apply takes over the ask's Link route: no second route choice, no local preflight, no second login. Apply opens its own session on that route, with its own 30 s budget and an empty read cache, so its re-plan reads everything again.
- Only in the same process, only once, only for the plan's server, and only when the configuration apply loads is identical to the one the ask logged in with (profile, Relay instance, route policy, Link generation and state, addresses). Otherwise, and always on the local route (it has no login to save), apply chooses its route as before. `ha-nova serve` always chooses again: its phone ask can be minutes old.
- Apply's own Link checks (secret-store match, device-credential read, Relay instance and Home Assistant user) are skipped; the ask's, made at its login before its re-plan and its wait (90 s CLI, 40 s MCP) for this configuration, stand in. The Relay still checks the device credential and the Supervisor the Ingress session on every request: a re-pair, a revoke, or a reinstalled App fails the first re-plan read, and nothing is sent. The device credential is the one the confirmation code was derived from.
- The Supervisor keeps an Ingress session for 15 minutes after its last use. If it is gone anyway, Link answers 401 before the Relay, the first re-plan read fails, nothing is sent, and the answer below applies.

### Failure after a human confirmed

- A failure that sent nothing after a Confirm in a window, on the phone, or in a form answers `CONFIRMED_NOT_SENT` (exit 1) instead of `RELAY_ERROR`: apply's session could not open, or a read or the write failed before reaching Home Assistant (the `errNothingSent` failures). Its `next`: nothing changed; read the target once with `ha-nova get --server <plan's server>` (a read does not follow the plan; MCP drops the flag, its session is pinned); if that read fails too, say so and don't run setup; never apply again unprompted; only after the user says they still want it, apply again (it asks them again), or plan again when the plan could not be kept.
- The same class on the dashboard path: a failed re-read right before the save (after the copy) and a save or create the Relay refused before forwarding returned a plain error, so they read as `RELAY_ERROR` and used up the plan though nothing changed. They are nothing-sent failures now: the plan is handed back, as for config and service plans, and after a Confirm they answer `CONFIRMED_NOT_SENT`.
- Its `say` is `say.confirmed_not_sent` in the plan's language (en: "Not done: you confirmed, but HA NOVA couldn't reach Home Assistant, so nothing changed."; de: "Nicht erledigt: Du hast bestätigt, aber HA NOVA hat Home Assistant nicht erreicht. Es wurde nichts geändert.").
- A failure after the write may have reached Home Assistant stays `OUTCOME_UNKNOWN` (exit 7: verify, never retry). Every other code and the phone's result texts are unchanged. A code typed in chat keeps `RELAY_ERROR`: retrying is safe there, and nobody is asked again. A chat app (`ha-nova serve`) keeps `RELAY_ERROR` and its own step, which says nothing changed and never names a terminal command.

### Known limits

- The ask's own login still runs before its wait starts (unchanged by this follow-up). With `ha-nova mcp`, a login that needs its retry (over 20 s) plus a tap in the last seconds can still pass 60 s. Counting the login into the wait needs the phone lookup's minimum to move with it (`discoverInTime`); left for a later change.
- Apply's own reads, its write (30 s), and its verify keep their own bounds, as on the local route.
- After an MCP form, apply still chooses its route: the form can wait up to 5 minutes, and its credential read is a session of its own.
- "The user confirmed" also covers a form answered with a wrong code: the session or the re-plan can fail before the code check. A new apply asks again either way.
- The `next` names `apply --plan` without `--no-snapshot`, as the window's and phone's own steps do; apply then asks about the copy again.

### Tests (each fails on fc7896f5)

- Window, phone, and both: a Confirm over Link costs one route choice (the ask's), not two.
- Confirmed, then a read fails, apply's session cannot open, or a dashboard's re-read after the copy fails: `CONFIRMED_NOT_SENT`, nothing sent, the plan kept, a `next` that reads first, its MCP form without `--server`, and the approved `say` in English and German.
- Without a human ask, the dashboard re-read failure keeps the plan, and the same apply then succeeds.
- Guards that pass on base too: a local route, or a configuration changed while the window was open, is chosen again; a write with an unknown outcome stays `OUTCOME_UNKNOWN`; a chat app keeps `RELAY_ERROR`.
