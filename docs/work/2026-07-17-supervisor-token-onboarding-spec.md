# Supervisor-Token Onboarding Spec

Status: active
Date: 2026-07-17
Trigger: live macOS first-install acceptance exposed that the Wave 6 wizard still requires a manually created Home Assistant Long-Lived Access Token (LLAT)

## Problem

Wave 6 removed manual Relay-token handling from the normal Home Assistant OS/Supervised flow, but the wizard still stops before pairing and asks the user to create an LLAT, paste it into the App configuration, save, and restart. This contradicts the committed UX goal that normal setup asks for only the Home Base pairing code.

Home Assistant already grants the App `homeassistant_api: true`. Official App communication documentation defines `SUPERVISOR_TOKEN` as the bearer credential for both `http://supervisor/core/api` and `ws://supervisor/core/websocket`. Requiring an additional user-created LLAT in that distribution is unnecessary.

## Goal

Make the normal Home Assistant OS/Supervised setup require no manually created Home Assistant token. After App installation and start, the only secret entered in the CLI is the six-digit Home Base pairing code.

## Scope

- Prefer `SUPERVISOR_TOKEN` for upstream REST and WebSocket authentication when the Relay runs as a Home Assistant App.
- Route that token through the Supervisor Core proxy endpoints.
- Retain `HA_LLAT` as the explicit standalone Container/Core authentication path and as a development compatibility fallback.
- Remove `ha_llat` from the App configuration UI/schema. Schema migration may discard the obsolete App-side copy, but NOVA never prints it or revokes the user-created Home Assistant token.
- Remove the LLAT walkthrough from the normal pairing wizard. The install step starts the App, then proceeds directly to Home Base pairing.
- Keep explicit-token/service/standalone flows compatible and keep their upstream-token responsibility explicit.
- Update active architecture, App, onboarding, and safety documentation so the distribution-specific auth model is unambiguous.

## Non-goals

- OAuth or a new Home Assistant auth provider.
- Sending `SUPERVISOR_TOKEN` to the CLI, Home Base, logs, App options, or pairing endpoint.
- Changing Relay-token generation, pairing-code semantics, or OS credential storage.
- Automatically deleting legacy LLATs or revoking user-created tokens.

## Security and compatibility

- `SUPERVISOR_TOKEN` remains process-local inside the App and is used only as the upstream bearer credential.
- App inbound authentication remains the independent Relay token returned through the existing one-time pairing exchange.
- Explicit `HA_URL` remains authoritative. Without one, Supervisor auth selects `http://supervisor/core`; LLAT auth selects the standalone default.
- When both upstream credentials exist, Supervisor auth wins in the App so a stale/revoked legacy LLAT cannot break an otherwise healthy supervised install.
- Startup fails loudly when neither credential exists.
- Existing App installations may retain an unknown legacy `ha_llat` option until Supervisor reapplies the new schema. NOVA ignores it; schema-aware deployment tooling drops that obsolete App-side copy without attempting to revoke the underlying Home Assistant token.

## Verification

- Unit/contract tests: token precedence, URL selection, REST URL, Supervisor WebSocket URL, missing-auth failure, App schema, run script, wizard steps/navigation, and absence of normal-flow LLAT instructions.
- Full repository verification.
- Live App acceptance on the maintainer Home Assistant instance: update App, confirm healthy `/health` and `/ws` with the LLAT option absent or unused, then exercise Home Base pairing from an isolated local client setup.
- Release rehearsal is mandatory because the change touches Go onboarding and Relay delivery/authentication.
- Public installer verification and exact reviewed-SHA release gates remain mandatory.

## Research basis

- <https://developers.home-assistant.io/docs/apps/communication/>
- <https://developers.home-assistant.io/docs/apps/configuration/>
- <https://developers.home-assistant.io/docs/api/supervisor/endpoints/>
