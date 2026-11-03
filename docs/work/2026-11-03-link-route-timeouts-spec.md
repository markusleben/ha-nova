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

### Retries: exactly these, each at most once

| # | What | When | Why it is safe |
|---|------|------|----------------|
| R1 | The Link login: OAuth refresh, WebSocket checks, Ingress session | A network or timeout error, or a server-side 5xx; at least 3 s left | No functional request exists yet. A refresh grant only mints a short-lived access token, the WebSocket calls read, and an unused Ingress session expires on its own. |
| R2 | Any Link request whose connection never opened | DNS, TCP, or TLS failed and no connection was obtained (`GotConn` never fired); at least 1 s left | Nothing was written, so nothing reached the Relay. This holds for writes too. |
| R3 | A Link GET (`/health`, setup's info reads) | No answer, or a 502/503 that did not come from the Relay; at least 1 s left | A GET only reads. |
| R4 | A session read over Link (`relaySession.sendIn`: the read allowlist, the phone lookup and listening probes, verify polls) | No answer from the Relay: no HTTP answer, Ingress unavailable, or a status Link or Home Assistant answered instead of the Relay; inside the same per-request deadline, at least 1 s left | Every caller of `sendIn` reads. Writes go through `sendWrite`. |

Never retried:

- a write (service call, config save or delete, dashboard or area write, snapshot save, phone notification) once its connection opened;
- anything that got an answer from the Relay;
- any authentication, identity (Relay instance, Home Assistant user), redirect, protocol, subscription, or secure-storage failure;
- anything on the local route (unchanged).

A TLS verification failure counts as a connection that never opened, so R1–R3 may repeat it once; every attempt verifies before anything is sent.

R2 can sit under R3 or R4, but an R2 attempt writes nothing: a session read reaches the Relay at most twice, a login's reads at most four times (R1 around R3), and a write at most once.

This narrows the 2026-07-25 non-goal "No automatic retry after a functional request may have reached the Relay". A read on the allowlist (R3, R4) may be sent once more; a write never.

### Remembered route (automatic only)

- `<cache>/link-route.json` records the profile id, the Relay instance id, the Relay's local address, and an end time at most 2 minutes ahead.
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
- Each process still logs in to Link once per session, so apply's two sessions log in twice. Follow-up: reuse the verified session inside one process (the 2026-07-25 spec allows that).
- A late Confirm over Link: apply chooses its route again after the window (90 s CLI, 40 s MCP), so a tap near the end plus a slow login and a slow verify can pass the host's limit (120 s, 60 s). The write is still sent once and verified; the agent may miss the result. Follow-up: hand the asking session's route to apply.
- When apply's session fails after the human confirmed, the agent gets a plain `RELAY_ERROR` (nothing was sent; the plan is not claimed). Follow-up: a `next` that says so.
- Doctor and setup's device check also use a remembered route for up to two minutes.
- Through an HTTPS proxy the TLS handshake keeps a fixed 5 s (`--max-time` still bounds it). Setup and cleanup calls to Link get the same 5 s connect limit.

## Tests (each fails on 8cf3bb1a)

- Health: the selection deadline is `--max-time`, and the connect budget reaches the resolver.
- Slow Link connect: a connection that does not open in time fails at the connect budget, not at the transport default.
- Login fails once (network), then succeeds: one command, two logins, success. Authentication and identity failures are not retried.
- Local unreachable, then Link: a second automatic selection within 2 minutes skips the preflight; another Relay instance or a far end time is probed again. When Link fails on a remembered route, the local Relay is probed in the same command and used when it is back.
- `relay ws|core` gets its full request time after a slow route choice; `trace latest` chooses its route once.
- A write whose request was sent is never retried. A write whose connection never opened is sent once. A session read goes once more only without an answer from the Relay; a local read never.
- Guards that pass on base too: a security failure, a shortened probe, or a probe that connected remembers nothing; the caller's own deadline keeps the route; cleanup still uses a healthy local Relay; a rejected grant is final.
