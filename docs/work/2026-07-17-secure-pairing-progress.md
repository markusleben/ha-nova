# Secure device pairing — build progress (integration branch)

Status: in progress. Branch `feat/secure-onboarding-integration` off origin/main.
Nothing merged; the whole feature is being proven locally first (user decision
2026-07-17: prove end-to-end before any PR/merge/release).

## Proven (hard, local)
- Supervisor-token upstream works for ALL used HA ops (87/90 live, incl. automation/
  script/helper create+delete) — the passwordless App path is real.
- Three cross-language crypto interop surfaces CONFIRMED JS relay <-> Go client:
  OPAQUE (RFC 9807, ristretto255/SHA-512, Argon2id 3/65536/4, salt=16 zero, len=64),
  TLS self-signed ECDSA P-256 + SHA-256 SPKI pin (TLS 1.3), pairing AEAD
  (HKDF-SHA-512 -> directional AES-256-GCM, AAD-bound).
- Full cross-language pairing LIFECYCLE against the real relay handlers: pair over
  HTTP bootstrap -> device cred refused on plain HTTP (403) -> activate over pinned
  TLS -> functional call -> revoke-self -> 401. (scratchpad/pair-e2e)

## Built + tested (relay side; ~256 tests green in touched areas)
- Modules (all <300 LOC): storage/atomic-file, security/{opaque-server, tls-identity,
  device-credential, device-registry, pairing-crypto, pairing-rate-limit, pairing-v1,
  principal}, http/handlers/{pair-v1, device-auth}, ha/supervisor-client,
  runtime/listeners.
- server.ts refactored: createRequestListener (pluggable auth + json/form/none body
  policy); createHttpServer stays a backward-compatible single-token wrapper.
- Layer 1 supervisor-token runtime ported (ha_llat kept as legacy-migration-only
  schema field — deviation from the dirty draft, on purpose).
- Adversarial review done: 6 real findings fixed with regression tests (incl. a HIGH
  TLS-identity silent-pin-rotation on partial /data restore).
- check-docs.sh relay LOC ceiling raised 3900->6500 (security surface, justified).

## Remaining (plumbing + UI + deploy; no crypto/interop risk left)
1. start.ts app-mode wiring: build registry (corrupt -> graceful, don't crash the
   owner UI) + TLS identity + supervisor secure port + pairing manager, and listen on
   8791 (bootstrap) / 8792 (TLS device) / 8793 (ingress). Remove the startup
   pairing-code log. Standalone stays single-listener.
2. config.yaml: add 8792/tcp port, ingress_port 8793, panel_title "NOVA",
   panel_icon mdi:star-four-points (update config-contract test accordingly).
3. Legacy migration on startup: import /data/relay_auth_token or option into the
   registry as a digest, readback-verify, clear option via self/options, tombstone.
4. NOVA owner page (ingress): owner gate via config/auth/list, CSRF form actions,
   generate/cancel code, device list + revoke, legacy revoke, LLAT cleanup, update
   status via self/info. Server-rendered, no JS.
5. Real Go CLI (setup_pairing.go): /pair/v1 flow, SPKI-pinned TLS client, keyring
   slots (current + pending), client_install_id, pending->activate->promote,
   re-pairing, doctor, uninstall/purge. Reuse scratchpad/pair-e2e/go as the basis.
6. Deploy to an ISOLATED test add-on on the real HA (own slug/ports/data) + prove a
   real cross-platform pairing; then slice into the reviewed PR sequence.

## Consumed-response persistence
Currently in-memory (idempotent retry within a process). File-backed store for
restart-after-finish resumability is a follow-up before release.

## 2026-07-17 (evening): full local proof + CLI completion — DONE
All six remaining items above are complete and live-proven on the real HA
(isolated test add-on `local_ha_nova_relay_test`, ports 18791/18792, own data):

- Real pairing lifecycle on real hardware: NOVA page code -> `ha-nova pair` ->
  OPAQUE -> device credential -> activation over pinned TLS -> functional
  skill ops (health, WS registry list, REST /api/config, helper create/delete)
  -> owner revoke on the NOVA page -> 401 -> re-pair with a fresh code.
- NOVA page polish: HA theme tokens (prefers-color-scheme, no JS), inline SVG
  star, explanatory copy, PRG redirect keeps the ingress trailing slash, pure-HTML
  auto-refresh (meta refresh only while a code is active). User-confirmed.
- Wizard: LLAT walkthrough fully removed (4-step wizard, Layer-1 port merged with
  the v1 pairing integration); paired installs re-run setup as verify-first (no
  fresh code when adding clients); pair-only configs (no HA address) handled.
- Doctor: transport-aware (device credential over pinned TLS, secure health base,
  re-pair guidance on 401, paired-but-missing-credential guidance, legacy installs
  get a one-line passwordless upgrade hint; --quiet suppresses it).
- Uninstall: full purge revokes the device pairing (401 = success), removes both
  keyring slots, NOVA-page hint when the relay is unreachable; standard keeps the
  pairing and says so.
- Guided relay update + /core requests route through the functional transport.
- Bootstrap/e2e scripts: no HA_LLAT requirement, but a stored legacy option
  survives the options rewrite (read-only passthrough).
- Full `npm run verify` green (734 TS tests + full Go suite + contracts);
  cross-compiles verified for windows/amd64, linux/amd64+arm64, darwin/arm64.

Remaining before PR slicing: final full-wizard live pass with a fresh code
(user supplies the code), adversarial review round on the new CLI work, then
slice the integration branch into the PR sequence (PR1 CI workflow first).

## Known test-infra issue (2026-07-18, open): flaky deletion of windows-installer-contract.test.ts
During SOME full-suite runs (npm run verify 3/3 at one point, one full `go test ./...`),
`tests/onboarding/windows-installer-contract.test.ts` gets deleted from the
worktree. Forensics so far: reproduced once isolated via
`TestInteractiveSetupBackFromRelayInstallLetsUserChangeHost` (1/3 runs; the test
drives the real wizard incl. real LAN discovery and unstubbed client sync with
HA_NOVA_DEV_ROOT=worktree), timing-correlated once with the pwsh preflight probe;
NOT reproducible isolated afterwards (0/9 with traps), survives idle, and once
disappeared despite chflags uchg (which provably works on this volume). No
product code path touches tests/; the deleter is test infrastructure or an
interaction between concurrently running tests. Containment (active):
- the file is tracked; `assert-vitest-files-exist` fails `npm run verify` loudly
  whenever it goes missing (proven tripwire, caught every occurrence);
- commits use explicit paths / git-status review, never blind `git add -A`;
- PR slicing ports hunks explicitly into fresh worktrees, so the deletion cannot
  leak into PRs.
Root-cause hunt is time-boxed; revisit when slicing PR 5 (the culprit test's
own PR) — run it under fs tracing with root privileges if available.

## Deploy lesson (2026-07-18): local test add-on version drift
The overlay's config.yaml version (0.7.0-test) drifted from the installed
version (0.6.0). In that state Supervisor treats the local add-on as having a
PENDING UPDATE and `ha apps rebuild` becomes a SILENT NO-OP (container start
time proved no restart for ~9h of "deploys"). Correct flow with drift:
`ha store reload` + `ha apps update <slug>` (builds from current source and
restarts). Keep store/installed versions aligned, or always deploy via update.
User-visible symptom that uncovered it: the stale "A device was just connected"
notice survived a hard reload (in-memory state cannot survive a real restart).

## 2026-07-18 00:45 — FINAL PROOF COMPLETE
Fresh-wizard live run from a factory-clean environment on the real HA, driven
by a user-generated code (803089): pair (one code, nothing else) -> secure
verify over pinned TLS -> skills installed -> "Setup complete!" (exit 0).
Post-state: doctor green over the device transport, WS skill op returns real
data, registry holds exactly one active device, pairing survives a relay
restart (registry on /data; WS reconnects on demand — known lazy behavior).
UX polish from user feedback shipped: consumed notice decays after 2 min;
one click selects the pairing code for copying (user-select:all, no JS).
=> Feature fully proven end-to-end. Next phase: PR slicing per plan (PR1 CI
workflow first), full AGENTS.md merge checklist per PR.
