# macOS Keychain Onboarding (Enduser-First) Mini Spec

Date: 2026-02-26

## Goal

Provide a low-friction onboarding path for macOS users that does not require editing `.env.local`.
Secrets must be stored in macOS Keychain by default.
Contributor-only `.env.local` remains supported but is no longer the recommended user path.
End-user onboarding must not include SSH/bootstrap contributor steps.

## Scope

In scope:
- One interactive onboarding script for macOS.
- Keychain-backed storage for `RELAY_AUTH_TOKEN` and optional `HA_LLAT`.
- Non-secret local config file for host/URL/key-path values.
- Helper commands to:
  - print runtime env exports (`env`),
  - launch client with injected env (`codex`, `claude`).
- Docs update for user onboarding and pointer from contributor docs.

Out of scope:
- Windows/Linux onboarding flow.
- Public Store install flow.
- Additional secret managers (1Password, Vault, etc.).

## Constraints

- Terminology: App + Relay (no Bridge wording in new docs/scripts).
- English-only project content.
- KISS/DRY.
- No backward-compatibility fallback additions before public release.

## Design

### Script path and commands

Create `scripts/onboarding/macos-onboarding.sh` with subcommands:
- `setup` (default): interactive prompt + persist config + save secrets to Keychain.
- `doctor`: preflight diagnostics for config/Keychain/HA/Relay reachability.
- `env`: print shell-safe `export` lines from stored config + Keychain.
- `codex`: execute `codex` with injected env.
- `claude`: execute `claude` with injected env.

### Storage model

- Keychain service names:
  - `ha-nova.relay-auth-token`
  - `ha-nova.ha-llat`
- Config file:
  - `~/.config/ha-nova/onboarding.env`
  - stores only non-secrets (`HA_HOST`, `HA_URL`, `RELAY_BASE_URL`).

### UX defaults

- `setup` prompts with sensible defaults:
  - `HA_HOST`: `homeassistant.local`
  - `RELAY_BASE_URL`: `http://<HA_HOST>:8791`
- `RELAY_AUTH_TOKEN` prompt allows empty input to reuse existing Keychain token first, otherwise auto-generate a secure random token.
- `HA_LLAT` stays optional.
- `codex`/`claude` run `doctor`-style preflight and abort if core checks fail.

### Error handling

- Fail fast on non-macOS or missing `security` CLI.
- Fail fast if required keychain secret is missing for `env`/launch commands.
- Classify relay health failures for better UX:
  - `401/403`: relay token mismatch
  - `404`: wrong Relay URL or App not started
  - `000`: host/port/network unreachable
- Treat `ha_ws_connected=false` as warning (degraded upstream WS scope), not hard failure.
- Emit concise actionable messages.

## Tests

Add contract test:
- `tests/onboarding/macos-onboarding-script-contract.test.ts`
- Validate script exists/executable/shebang.
- Validate Keychain commands are present (`security add/find generic password`).
- Validate subcommands and npm wiring (`onboarding:macos`), with no SSH/bootstrap coupling.

## Documentation

- Add `docs/user-onboarding-macos.md` with user flow.
- Update `docs/contributor-deploy-loop.md` with short pointer to user onboarding doc.
- Record decisions in `docs/choices.md` and change notes in `docs/breadcrumbs.md`.
