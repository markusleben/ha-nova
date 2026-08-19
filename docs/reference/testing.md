# Testing HA NOVA

Four layers, cheapest first. Use the smallest one that covers your change.

| Layer | Proves | Needs | Command |
| --- | --- | --- | --- |
| 1. Host-safe checks | Types, contracts, unit + Go behaviour | Nothing (never touches real secure stores or a browser) | `npm run verify` |
| 2. Disposable-HA E2E | The relay against a **real** Home Assistant | Docker + Python 3 (`pip install websockets`) — **no HA of your own** | `bash scripts/e2e/disposable-ha/run.sh` |
| 3. Live local against your own HA | The local onboarding wizard + device pairing on real hardware | An HA instance you control | See [Live against your own HA](#3-live-against-your-own-ha) |
| 4. Home Assistant Cloud Beta gate | OAuth, native storage, Supervisor Ingress, user binding, routing, and lifecycle on real services | Home Assistant OS/Supervised, active Home Assistant Cloud remote access, and supported desktop OSes | See [Home Assistant Cloud Beta real-device gate](#4-home-assistant-cloud-beta-real-device-gate) |

Layer 1 is documented in [CONTRIBUTING.md → Verification Matrix](../../CONTRIBUTING.md#verification-matrix); this page covers layers 2 through 4, plus the isolation env vars and the safety rules that keep them from touching anything you care about.

## 1. Host-safe checks

`npm run verify` is the canonical gate: dependency audit, blocked-file checks, typecheck, docs contracts, the core Vitest slice, onboarding contracts, the host-safe build, the Go CLI tests, and release contracts. It must never open a browser or touch a real OS keyring. Pick the smallest matching slice (`verify:docs`, `verify:onboarding`, `verify:release-contracts`, …) from the Verification Matrix; fall back to `npm run verify` when a change crosses boundaries.

## 2. Disposable-HA E2E (no HA of your own)

This is the path for contributors **without** a Home Assistant instance.

```bash
pip install websockets   # one-time; used by the token-minting step
bash scripts/e2e/disposable-ha/run.sh
```

It boots a real, throwaway Home Assistant in containers, completes its onboarding through the API, mints a long-lived token, starts the standalone relay against it, asserts the guarantees only a live system can prove (auth enforced, version reported, no lazy WebSocket connect, real REST/WS proxy data, bounded subscriptions), and **destroys everything afterwards**. Nothing of yours is touched.

CI runs the same script weekly and on demand via `.github/workflows/e2e-disposable-ha.yml` (`workflow_dispatch`), so you can also trigger it from GitHub without a local Docker setup.

## 3. Live against your own HA

For maintainers with an HA instance, when you need to exercise the interactive setup wizard, the NOVA owner console, or the OPAQUE + TLS device pairing on real hardware. **Two hard rules:** deploy an *isolated* test add-on (never your production one), and run an *isolated* CLI (never your real config/keyring). Both are covered below.

### Deploy an isolated test add-on

`scripts/dev/ha-app-bootstrap.sh` syncs `nova/` to your HA over SSH and installs it as a local add-on (env: `HA_HOST`, `HA_SSH_KEY`, `RELAY_AUTH_TOKEN` required; `SSH_USER`/`SSH_PORT`/`APP_SLUG`/`SUPERVISOR_SLUG` optional, also read from `.env` / `.env.local`).

**Isolation is not automatic.** The script syncs `nova/` verbatim, so the deployed `config.yaml` keeps the production `slug`, `name`, and host ports `8791`/`8792`. `APP_SLUG` only changes the target directory — it does **not** edit `config.yaml`, so on its own it collides with your production NOVA Relay on the same ports. To deploy a genuinely separate instance, work from a **copy** of `nova/` whose `config.yaml` you have edited:

- `slug:` and `name:` → distinct (e.g. `ha_nova_relay_test`, `NOVA Relay TEST`)
- `ports:` → unused host ports (e.g. `18791`/`18792`)

Then `rsync` that overlay to `/addons/local/<slug>/`, `ha store reload`, `ha apps install local_<slug>`. When the overlay's `config.yaml` version drifts from the installed one, `ha apps rebuild` becomes a silent no-op — use `ha store reload` + `ha apps update local_<slug>`. Never `docker restart` an ingress add-on from outside (its IP changes and HA's ingress route breaks); only `ha apps …` commands. `scripts/smoke/ha-app-e2e.mjs` runs a Node smoke pass against the deployed app.

### Run an isolated CLI

**`export` the isolation vars once** so every CLI command in the session — not just `setup`, but `pair`, `relay`, and especially `uninstall --purge` — inherits them. Without this on the follow-up commands, they read/write your **real** config and keyring, and the documented purge would delete your production token and device credential:

```bash
export HOME=/tmp/nova-test-home
export HA_NOVA_DEV_ROOT="$PWD"           # run the local build, not a released bundle
export HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1                     # relay token → file, not the
export HA_NOVA_TEST_KEYRING_FILE=/tmp/nova-test-token            #   real ha-nova.relay-auth-token slot
export HA_NOVA_NO_BROWSER=1
export HA_NOVA_NO_CENSUS=1
- Production census isolation (#446): no test, smoke, release, or
  deployment-verification path may call the production Worker's ping or
  withdraw endpoints; functional census checks run only against the isolated
  test Worker — see `docs/reference/census.md` → Production isolation.

scripts/onboarding/bin/ha-nova pair --credential-store=file --relay-url http://<ip>:18791 --code NNNNNN
scripts/onboarding/bin/ha-nova setup claude --relay-url http://<ip>:18791
scripts/onboarding/bin/ha-nova relay health          # exercise skills over the device transport (also core|ws|trace)
scripts/onboarding/bin/ha-nova uninstall --purge --yes   # verifies the server-side revoke + local cleanup
```

`--credential-store=file` stores device credentials below the isolated `HOME`.
The relay-token vars are **mandatory even for pure device pairing**: `HOME`
does not namespace the legacy relay-token keyring service.
`HA_NOVA_KEYRING_SERVICE=<unique-name>` is the alternative if you want a
throwaway keyring entry instead of a file.

`HA_NOVA_TEST_SECRET_DIR` is an injected `go test` seam, not a CLI isolation
interface. Normal development and release binaries deliberately ignore it.

### Isolation env vars

| Variable | Effect | Use for |
| --- | --- | --- |
| `HA_NOVA_DEV_ROOT=<repo>` | Runs the CLI in dev mode — uses the repo's local skills/bundle instead of a released bundle | Any local build |
| `HA_NOVA_CONFIG_DIR=<absolute-path>` | Relocates config, checkpoints, state, and census data without changing the OS login home | Native Cloud-secret tests that must keep the real desktop keyring |
| `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` + `HA_NOVA_TEST_KEYRING_FILE=<path>` | The legacy relay-auth token is stored in a file instead of the OS keyring | Isolate the token slot on a desktop |
| `HA_NOVA_KEYRING_SERVICE=<name>` | Overrides the relay-token keyring service name | Isolate the real token slot by name |
| `HA_NOVA_NO_BROWSER=1` | `setup` never opens a browser | Scripted / headless runs |
| `HA_NOVA_NO_CENSUS=1` | Suppresses Census prompts, observations, reports, and withdrawals | Every development, contributor, CI, and E2E run |
| `HA_NOVA_NO_UPDATE_NUDGE=1` | Suppresses the background update check | Deterministic test output |

## 4. Home Assistant Cloud Beta real-device gate

Host-safe tests prove parsing, lifecycle, redirect rejection, no-UI policy, and
transport selection with injected stores and servers. They do not prove a
native keyring, a Nabu Casa OAuth flow, or a real Supervisor Ingress session.
The Cloud Beta therefore requires an exact-candidate evidence envelope plus
every real-device qualification invalidated by the candidate diff.

A candidate whose delta matches an invalidation-map row with real-platform
scope uses its downloaded binary with `HA_NOVA_NO_CENSUS=1` to run one
reference-platform `relay health --via cloud` against the exact installed
Relay App. The proof parses the JSON and requires the expected App version
plus `ha_ws_connected: true`; command success alone is insufficient. This
smoke is separate from full route parity; maintenance deltas refresh the
envelope and provenance without it. When no validation infrastructure is
available, the risk-scope spec's reference-smoke waiver may replace the
smoke with a ledger-recorded maintainer decision.

This isolation procedure serves the real-device qualification runs below. The
exact-candidate provenance check and health smoke follow a different layout:
the `SMOKE_HOME` procedure in `docs/releasing.md`, because Unix provenance
resolves the installed bundle from `$HOME/.local/share/ha-nova` and the smoke
profile isolates its own authorization there.

For qualification runs, use an isolated CLI as described above, but do not set
a file-backed device credential or test-keyring override for a release proof.
Cloud OAuth refresh tokens have no production file backend or
environment-variable override. Keep the real login `HOME`: macOS Keychain and
Linux Secret Service resolve the interactive user's native store from that
desktop identity. Relocate HA NOVA's non-secret config and checkpoints with
`HA_NOVA_CONFIG_DIR`, and additionally set a cryptographically unique
relay-token service before the first command:

```bash
cloud_test_root="$(mktemp -d)"
export HA_NOVA_CONFIG_DIR="${cloud_test_root}/config"
export HA_NOVA_KEYRING_SERVICE="ha-nova.relay-auth-token.cloud-beta.$(openssl rand -hex 16)"
export HA_NOVA_NO_CENSUS=1
unset HA_NOVA_TEST_SECRET_DIR
unset HA_NOVA_ALLOW_INSECURE_TEST_KEYRING
unset HA_NOVA_TEST_KEYRING_FILE
```

Use cryptographically unique explicit server profile names. Keep the same
`HA_NOVA_KEYRING_SERVICE` value for the complete run and cleanup. Build one
stable candidate binary path and reuse that exact path so caller-scoped macOS
Keychain authorization is not invalidated. A macOS development build must also
carry the hardened-runtime flags enforced by the native-secret worker:

```bash
cloud_test_binary="${cloud_test_root}/ha-nova"
( cd cli && go build \
  -tags cloudremote_dev \
  -ldflags "-X main.cloudRemoteDevAppSlug=local_<isolated-slug>" \
  -o "${cloud_test_binary}" . )

if [[ "$(uname -s)" == "Darwin" ]]; then
  /usr/bin/codesign --force --sign - \
    --options runtime,hard,kill,library \
    --timestamp=none \
    "${cloud_test_binary}"
  /usr/bin/codesign --verify --strict "${cloud_test_binary}"
fi
```

The injected App slug is accepted only by the compile-time development build
tag. The ad-hoc macOS identity above is suitable only for local development
proof; it is not stable release-signing evidence. Public builds do not contain
that activation path. Patch the isolated App overlay's `version.json` to set
`cloud_remote_enabled: true` and list only the desktop platform being
validated; never enable the production App for an unfinished gate.

Exercise both setup paths. Use a separate empty isolated CLI home/config for the
remote-first case:

```bash
# Existing secure local pairing: reuses and user-binds that device.
"${cloud_test_binary}" cloud add --server <test-profile>

# Remote-first: verifies the Cloud origin, then pairs through Ingress v2.
"${cloud_test_binary}" cloud add --server <remote-profile> --url https://<cloud-host>

"${cloud_test_binary}" cloud status --server <test-profile>
"${cloud_test_binary}" relay health --server <test-profile> --via local
"${cloud_test_binary}" relay health --server <test-profile> --via cloud
"${cloud_test_binary}" server route automatic --server <test-profile>
"${cloud_test_binary}" cloud reconnect --server <test-profile>
"${cloud_test_binary}" cloud remove --server <test-profile>
```

Required evidence is exact-target plus risk-scoped qualification, except the
ancestor-bound `uses:`-only and guarded non-sensitive source escapes
(`docs/releasing.md`). Repeat a real-device row only for first support or
after a relevant implementation
or evidence-harness change; exact-target CI, signed provenance on all enabled
OSes, and the installed Relay App always run, and the reference Cloud health
smoke runs for deltas with real-platform scope:

- `/health`, `/ws`, `/core`, `/files`, and `/backups` parity through a real
  Home Assistant Cloud route on one reference platform after a transport
  change;
- one real canonical Nabu Casa OAuth flow and one real standard
  non-administrator binding; Home Assistant owns the MFA challenge before
  returning the same OAuth callback, while deterministic tests cover custom
  origins, inactive subscription, disabled remote access, and authorization
  abort;
- one isolated Cloud-authorized profile first proves Relay App restart and
  reinstall recovery, then HA NOVA CLI standard uninstall/reinstall with
  retained authorization, then full purge last; the purge revokes and verifies
  the active remote authorization and device before local cleanup;
  deterministic tests cover durable-state recovery and concurrent reconnect,
  while update and instance mismatch run for relevant changes;
- redirect rejection and absence of credentials from config, argv, logs,
  diagnostics, and AI-visible output at every network hop;
- `automatic` routing chooses Cloud only for a pure local network failure and
  never after authentication, pin, identity, protocol, or dispatch failure;
- one 10,000-command Ingress-session stress run with bounded memory after a
  Cloud or Relay transport change or stress-harness change, not once per
  operating system;
- real native-storage happy-path and fail-closed no-UI behavior on every
  advertised desktop OS for first support; shared orchestration changes repeat
  one reference OS and adapter changes repeat only the affected OS;
  deterministic platform tests cover cancellation and timeout.

Standard uninstall must keep the test profile, device pairing, and Cloud
authorization. Full purge must revoke and verify the Cloud authorization before
removing local config or secrets. If any security, full-parity, or native-store
gate fails, the Beta is not eligible for release. The complete contract lives
in `docs/work/2026-07-30-cloud-release-evidence-risk-scope-spec.md`.
The activation or release pull request carries the non-secret qualification
ledger required there; no private Cloud URL belongs in it.

## Headless and cross-platform

A headless box (container, server, LXC, SSH into a desktop) has no Secret Service, so the CLI stores the device credential in a private `0600` file selected by a `.file-backend` marker — automatically, no flags. To test that path, run the Linux binary in a bare container against a reachable relay:

```bash
# The CLI is its own Go module — build from cli/. Pick GOARCH for your Docker host.
( cd cli && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/ha-nova-linux . )
docker run -d --name nova-linux-e2e \
  -v /tmp/ha-nova-linux:/usr/local/bin/ha-nova:ro \
  -v "$PWD:/opt/ha-nova-src:ro" -e HA_NOVA_DEV_ROOT=/opt/ha-nova-src \
  debian:bookworm-slim sleep 3600
docker exec nova-linux-e2e ha-nova pair --relay-url http://<ip>:18791 --code NNNNNN
```

That device-credential fallback does not apply to Home Assistant Cloud OAuth.
Cloud setup is desktop-only and fails closed in SSH, WSL, containers, services,
gateways, and other headless sessions; the local transport remains available.

Cross-compile for every target with the same `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build` from `cli/` (windows/darwin/linux × amd64/arm64). `scripts/smoke/linux-headless-setup-check.sh` captures the remote Secret Service preflight over SSH for a Linux release proof.

## Safety rules

These are non-negotiable when testing on real machines:

- **Never touch the production App, keyring, or credentials.** For the App, use a distinct slug/ports. For local-only CLI tests, use a throwaway `HOME`, pair with `--credential-store=file`, and redirect the relay token (`HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` + `HA_NOVA_TEST_KEYRING_FILE`, or a unique `HA_NOVA_KEYRING_SERVICE`) — without the last one, `uninstall --purge` still deletes your real `ha-nova.relay-auth-token` keyring entry. For native Cloud-secret tests, keep the login `HOME`, set an isolated `HA_NOVA_CONFIG_DIR`, and use unique profile and keyring-service names; use a separate OS user or VM when the real-device gate requires stronger isolation.
- **Never report Census data from tests.** Export `HA_NOVA_NO_CENSUS=1` for every development, contributor, CI, and E2E process, including child processes.
- **Only create your own test objects** on a live HA (helpers, automations you made); leave existing objects read-only.
- **Clean up afterwards** — `ha apps uninstall <test-slug>`, remove the container, and run a leftover scan (0 test objects). Never leave a test App running on someone's HA.
- **A green CI/host-safe run is not a release proof.** Releasing still needs the live checks above on the exact commit being tagged (see [docs/releasing.md](../releasing.md)).
