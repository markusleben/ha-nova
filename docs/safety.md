# Safety guarantees

What HA NOVA promises, what enforces it, and what proves it. Every row names a file you can read and a test you can run — no adjectives.

Last verified: 2026-07-11 (`npm run verify`, 70 test files / 650 tests green; `go test ./...` green).

## Writes

| Guarantee | Enforced by | Verified by |
|---|---|---|
| Nothing is written before you see a preview of exactly what will change | Safety Core, first bullet in every mutation skill (`docs/reference/skill-architecture.md` → Safety Core) | `tests/skills/skill-template-contract.test.ts` asserts the byte-identical block in all 13 mutation skills; `tests/skills/ha-safety-contract.test.ts` |
| Your confirmation binds to the shown preview and expires if the target, payload, endpoint, or scope changes | Safety Core bullet 2; context skill → Active Preview Confirmation | `tests/skills/ha-safety-contract.test.ts` (13 mutation skills pinned) |
| "Go ahead" said *before* a preview authorizes drafting only — never the write | Safety Core bullet 3 | `tests/skills/ha-safety-contract.test.ts` (all five pre-preview phrases pinned) |
| Deletes require a typed `confirm:<token>`; "yes" is not accepted, and a menu is never offered instead | Safety Core bullet 4; context skill → Confirmation Tiers | `tests/skills/write-delete-safety-contract.test.ts` ("keeps delete a typed token even under menu pressure") |
| Every write is verified by reading the config back, not by trusting the response | `skills/write/SKILL.md` Phase 3; `skills/ha-nova/write-safety.md` → Verification Honesty | `tests/skills/ha-cross-skill-integration.test.ts`; `tests/skills/write-delete-safety-contract.test.ts` |
| A verified update can be reverted — the last 5 changed targets, one step back each | `skills/ha-nova/write-safety.md` → Update-Revert; `skills/ha-nova/update-revert.md`; `cli/snapshot.go` | `cli/snapshot_test.go`; `tests/skills/write-delete-safety-contract.test.ts` |
| Where there is no revert (deletes, dashboards, scenes, registry ops), the skill says so *before* the write instead of implying safety | `skills/ha-nova/write-safety.md` → Safety-Mechanism Availability by Skill | `tests/skills/write-delete-safety-contract.test.ts` |
| An AI never writes to an unfamiliar Home Assistant API by trial and error | Safety Core bullet 7 (STOP → `ha-nova:fallback`); `skills/fallback/SKILL.md` → Anti-Patterns | `tests/skills/ha-safety-contract.test.ts` ("enforces fallback skill as mandatory for raw relay writes") |
| IDs are never guessed — they are resolved or you are asked | Safety Core bullet 5 | `tests/skills/skill-template-contract.test.ts` (Safety Core verbatim) |

## Tokens and privacy

| Guarantee | Enforced by | Verified by |
|---|---|---|
| Your Home Assistant token stays on the server. The AI client never receives it | The relay resolves it from its own environment (`nova/src/security/token-resolver.ts`); no code path sends it downstream | `nova` relay tests (`tests/security/token-resolver.test.ts`); `scripts/check-docs.sh` |
| The relay token is stored in your OS credential store (Keychain / Credential Manager / Secret Service), not in a config file | `cli/keyring_*.go` (service `ha-nova.relay-auth-token`); the plaintext file path exists only for headless `--service` installs | `cli/keyring_*_test.go`; `scripts/check-docs.sh` ("macOS Keychain integration") |
| No telemetry, no analytics, no phone-home | There is none to disable | `scripts/check-docs.sh` check [11] fails the build if telemetry patterns appear in `nova/src` |
| No cloud relay: your data goes from your machine to your Home Assistant, and nowhere else | The relay only talks to `HA_URL` (`nova/src/ha/*`); it cannot proxy other hosts | Relay tests; `docs/reference/bridge-architecture.md` |
| Camera frames stay in client-private scratch storage and are never sent onward | `skills/camera/SKILL.md` → Safety | `tests/skills/skill-template-contract.test.ts` |

## The relay itself

| Guarantee | Enforced by | Verified by |
|---|---|---|
| Every endpoint requires authentication — there is no unauthenticated surface, not even `/health` | `nova/src/http/server.ts` (auth runs before routing) | `tests/security/auth.test.ts`; `tests/http/*`; the container smoke test asserts a 401 |
| Token comparison is constant-time | `nova/src/security/auth.ts` (`timingSafeEqual`) | `tests/security/auth.test.ts` |
| Path traversal into non-`/api/` paths is rejected, including multi-round percent-encoding | `nova/src/http/handlers/core-proxy.ts` → `normalizeCorePath` | `tests/http/core-proxy.test.ts` (traversal vector suite) |
| Requests and responses are bounded (1 MiB in, 256 MiB out, 8 MiB for binary) | `nova/src/http/server.ts`; `nova/src/ha/rest-client.ts` | `tests/http/*`; `tests/ha/rest-client.test.ts` |
| The relay holds no long-lived subscriptions: event collection is bounded and always unsubscribes | `nova/src/ha/ws-client.ts` (`finally` unsubscribe, `max_events`/`timeout_ms`); bare subscriptions are rejected | `tests/ha/ws-client.test.ts`; `tests/http/ws-proxy.test.ts` |
| A subscription that never establishes fails loudly instead of looking like an empty result | `nova/src/ha/ws-client.ts` (window timer starts only after the ack) | `tests/ha/ws-client.test.ts` ("fails in window mode when the subscription is never acknowledged") |
| The relay contains no Home Assistant business logic — it cannot silently reinterpret your request | `nova/src/**` is transport only | `scripts/check-docs.sh` checks [3], [4], [5] fail the build on domain handlers, tool definitions, or new endpoint families |
| One relay codebase for both distributions (App and container) — no second implementation to drift | `nova/Dockerfile.standalone` builds from `nova/src` | `scripts/check-docs.sh` check [2b] |

## Honesty rules

These are guarantees about what HA NOVA *says*, which matter as much as what it does:

| Guarantee | Enforced by | Verified by |
|---|---|---|
| A successful write is never reported as verified behavior — persistence is not the same as "it works" | `skills/ha-nova/write-safety.md` → Verification Honesty ("never a bare 'verified'") | `tests/skills/write-delete-safety-contract.test.ts` |
| Conclusions are bound to evidence; without it, hypotheses are labelled as such | Context skill → Claim-Evidence Binding; `skills/diagnose/SKILL.md` | `tests/skills/ha-nova-contract.test.ts` |
| A notification that Home Assistant accepted is not claimed as delivered to your phone | `skills/notify/SKILL.md` | `tests/skills/skill-template-contract.test.ts` |
| An empty MQTT window means "nothing was published", and retained broker replays are never counted as live device traffic | `skills/mqtt/SKILL.md` | `tests/skills/skill-template-contract.test.ts` |
| Internal review codes (R-18, H-09, …) never reach you — findings are described in plain language | `skills/ha-nova/output-rules.md`; `skills/review/checks.md` → Output Guardrail | `tests/skills/skill-template-contract.test.ts` (check-code leak allowlist) |

## Reproduce it

```bash
npm run verify      # typecheck + full test suite + build + documentation fact-check
cd cli && go test ./...
```

The documentation fact-check (`scripts/check-docs.sh`) is deliberately hostile to its own README: it fails the build if the relay grows domain logic, endpoint families, telemetry, or a second implementation.
