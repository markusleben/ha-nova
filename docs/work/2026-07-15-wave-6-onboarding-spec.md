# Wave 6 Onboarding Spec

Status: complete
Date: 2026-07-15
Sequencing SSOT: `docs/work/masterplan-2026-h2.md` -> Wave 6
Release train: Relay 0.6.0; version/tag changes stay in release prep
Implementation PRs: #357, #358, #359, #360

## Goal

Make the normal HA OS/Supervised setup ask for one secret only: a six-digit pairing code shown inside Home Assistant. Keep the Relay transport-only, preserve existing installs, and give non-terminal users one in-product status surface.

## User experience

### New App install

1. The CLI discovers Home Assistant and guides the user through installing NOVA Relay.
2. The Home Assistant Long-Lived Access Token stays in the App configuration UI.
3. The App creates and persists its own relay token when no legacy token is configured.
4. Home Base shows a six-digit pairing code. The CLI asks for that code, exchanges it over the local Relay connection, stores the returned relay token in the existing OS credential store, and verifies `/health` plus `/ws`.
5. The relay token is never displayed, copied, passed in an argument, or entered in App configuration during the normal flow.

Existing saved CLI tokens and existing App `relay_auth_token` values continue to work. Container/Core remains an advanced explicit-token setup because it has no Supervisor ingress and its data directory may be ephemeral.

## Pairing protocol

`POST /pair` is the only route that does not use the relay bearer token. The pairing code is its credential.

Request:

```json
{"code":"123456"}
```

Success:

```json
{"ok":true,"data":{"relay_token":"<opaque token>"}}
```

Malformed shapes return `400 VALIDATION_ERROR`; a well-formed wrong, expired, or replayed code returns `401 PAIRING_FAILED`; an exhausted bucket returns `429 PAIRING_RATE_LIMITED` plus `Retry-After`.

Rules:

- Generate codes with `crypto.randomInt(0, 1_000_000)` and zero-pad to six digits.
- A code lives for ten minutes, is single-use, and rotates immediately after success or expiry.
- Compare fixed digests in constant time. Invalid and expired codes return the same generic failure.
- Allow at most five failed attempts per socket peer per 60-second fixed window and 30 failed attempts globally per five-minute fixed window. Track at most 256 peers and evict the oldest inactive entry at the cap. Return `429` with `Retry-After` capped to the remaining window when blocked.
- Do not trust forwarded client-address headers. The socket peer is the rate-limit identity.
- Set `Cache-Control: no-store` on every `/pair` response. Never log request bodies, codes submitted by clients, or the returned relay token.
- Keep state in memory. Restarting the Relay rotates the code, clears rate-limit buckets, and emits the one startup-code fallback allowed below. Expiry rotation is silent; Home Base is the source for the current code after startup.
- The endpoint performs no Home Assistant call and contains no entity, service, config, or domain logic.

The current code and its expiry appear in authenticated Home Base. The App log may announce the initial startup code once; submitted codes and later rotations never appear there. The relay token may appear in neither.

## Relay token ownership

- App distribution: prefer an existing non-empty `relay_auth_token` option for backward compatibility. Otherwise load or create a 32-byte random token in the App data directory with owner-only permissions and reuse it across restarts.
- Keep the legacy App option optional and clearly marked as advanced during this release train; new users leave it empty.
- Standalone distribution: continue requiring `RELAY_AUTH_TOKEN`. Do not silently generate an ephemeral container credential.
- Token rotation is out of scope. The pairing seam must allow adding it later without changing `/ws`, `/core`, `/files`, or `/backups`.

## Home Base

Enable Supervisor ingress on the existing Relay port with an admin-only sidebar panel and `/home` entry point.

- Serve one small HTML asset. It renders the same health snapshot used by `GET /health`: HA WebSocket connection state/reason, running Relay version, file-access mode, and snapshot-store status.
- Also show the current pairing code and expiry, the required Relay floor for this release train, and the stable macOS/Linux and Windows install one-liners.
- Gate Home Base on the normalized Supervisor ingress peer (`172.30.32.2`, including its IPv4-mapped IPv6 form) plus authenticated ingress user headers. Direct port access must not become an unauthenticated status surface merely by spoofing headers.
- Set a restrictive CSP, `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, and no external scripts, fonts, analytics, or telemetry.
- Keep status display read-only. Home Base does not call Home Assistant, pair a client, rotate a token, or mutate Relay/App settings.
- A charter contract pins that the page and pairing handler contain no Home Assistant business vocabulary or domain routing.

Official Supervisor behavior used by this design: ingress authenticates the Home Assistant user, provides `X-Ingress-Path`, and identifies the user with `X-Remote-User-*` headers. The Home Base handler still restricts its socket peer to the Supervisor ingress proxy instead of trusting those headers alone.

## CLI onboarding

### Pairing

- Add a focused pairing client that POSTs the code without putting it in argv, logs, or persisted config.
- Keep `--relay-token` for non-interactive, Container/Core, service, migration, and test flows.
- Existing complete setups reuse their stored token without prompting.
- On a relay-auth failure with a reachable Home Assistant, offer the Home Base/App deep link and pairing again. On an upstream auth failure, open `haProfileSecurityURL`; do not send users back to relay-token repair.

### Multi-instance discovery

- Return all reachable candidates in stable priority order instead of stopping at the first success.
- Deduplicate candidates by normalized resolved host.
- One candidate selects automatically unless its only confirmed address is an unresolved `.local` name; that case remains an editable default so the user can enter an IP. Multiple candidates show a pick list with address and discovery source. No candidate keeps the existing manual-address path.
- Bound the total discovery time and candidate count; no unbounded subnet scan.

### Non-TTY and Windows

- Installers that cannot start the wizard print the exact next command and tell the user that setup will ask for the Home Base pairing code; they never hang on hidden input.
- Windows installer preflight runs before downloads: supported Windows generation, PowerShell floor, supported process architecture, writable per-user install root, and TLS-capable GitHub access. Client-specific availability remains in the setup client picker.
- Failures name the unmet prerequisite and the recovery action before any install is replaced.

## PR boundaries

1. Relay pairing foundation: server-generated App token, pairing-code manager, `/pair`, security/charter tests, Relay reference updates.
2. Home Base: Supervisor ingress configuration, HTML/status handler, headers/peer gate, operator docs and contracts.
3. CLI pairing flow: exchange client, wizard replacement of the normal relay-token step, revoked-token/LLAT repair routing, targeted Go tests.
4. Onboarding flank: multi-instance pick list, non-TTY installer guidance, Windows prerequisite preflight, targeted platform tests.
5. Closeout: update the active spec/masterplan status and contract truth after all four behavior PRs are merged.

Each PR completes the repository merge checklist independently. No README feature claim, `version.json` floor bump, release notes, tag, or publish action lands before the separate release-prep gate.

## Acceptance

- New App flow succeeds without the user seeing or configuring the relay token.
- Legacy App option, saved CLI token, `--relay-token`, service-token-file, and Container/Core paths retain regression coverage.
- Wrong, expired, replayed, malformed, and rate-limited pairing attempts are covered; submitted codes, post-start rotations, and relay tokens are absent from logs and cacheable responses. Only the explicit initial startup-code announcement is allowed.
- Direct `/home` access is rejected while a correctly identified Supervisor ingress request renders a truthful status page.
- Multi-instance discovery does not silently choose one of several reachable homes.
- Token-revoked and LLAT-revoked failures route to different, correct recovery surfaces.
- Targeted Relay/CLI suites and full `npm run verify` pass.
- Every PR receives a real clean Codex result on its final SHA, all threads are resolved, and merge uses squash/delete-branch only after the mandatory checklist is complete.

## Completion record

- #357 added the persistent App token, pairing-code manager, `/pair`, rate limits, security contracts, and compatibility coverage.
- #358 added the Supervisor-ingress Home Base, authenticated peer/header gate, read-only status rendering, and charter contracts.
- #359 moved the normal CLI flow to Home Base pairing while retaining saved-token, explicit-token, service, legacy-relay, and LLAT-repair paths.
- #360 completed bounded multi-instance discovery, non-TTY guidance, Windows preflight, and the `.local` correction path.
- All four behavior PRs passed the full repository checklist with a real clean Codex result on the final SHA and all review threads resolved.
- No release, README claim, version bump, tag, or publish action is part of this closeout. Those remain release-prep work.

## Research basis

- Home Assistant App presentation and ingress: <https://developers.home-assistant.io/docs/apps/presentation/>
- Home Assistant App configuration: <https://developers.home-assistant.io/docs/apps/configuration/>
- Home Assistant ingress user headers: <https://developers.home-assistant.io/docs/apps/security/>
