# Testing HA NOVA

Three layers, cheapest first. Use the smallest one that covers your change.

| Layer | Proves | Needs | Command |
| --- | --- | --- | --- |
| 1. Host-safe checks | Types, contracts, unit + Go behaviour | Nothing (never touches real secure stores or a browser) | `npm run verify` |
| 2. Disposable-HA E2E | The relay against a **real** Home Assistant | Docker only — **no HA of your own** | `bash scripts/e2e/disposable-ha/run.sh` |
| 3. Live against your own HA | The full onboarding wizard + device pairing on real hardware | An HA instance you control | See [Live against your own HA](#3-live-against-your-own-ha) |

Layer 1 is documented in [CONTRIBUTING.md → Verification Matrix](../../CONTRIBUTING.md#verification-matrix); this page covers layers 2 and 3, plus the isolation env vars and the safety rules that keep them from touching anything you care about.

## 1. Host-safe checks

`npm run verify` is the canonical gate: dependency audit, blocked-file checks, typecheck, docs contracts, the core Vitest slice, onboarding contracts, the host-safe build, the Go CLI tests, and release contracts. It must never open a browser or touch a real OS keyring. Pick the smallest matching slice (`verify:docs`, `verify:onboarding`, `verify:release-contracts`, …) from the Verification Matrix; fall back to `npm run verify` when a change crosses boundaries.

## 2. Disposable-HA E2E (no HA of your own)

This is the path for contributors **without** a Home Assistant instance.

```bash
bash scripts/e2e/disposable-ha/run.sh
```

It boots a real, throwaway Home Assistant in containers, completes its onboarding through the API, mints a long-lived token, starts the standalone relay against it, asserts the guarantees only a live system can prove (auth enforced, version reported, no lazy WebSocket connect, real REST/WS proxy data, bounded subscriptions), and **destroys everything afterwards**. Nothing of yours is touched.

CI runs the same script weekly and on demand via `.github/workflows/e2e-disposable-ha.yml` (`workflow_dispatch`), so you can also trigger it from GitHub without a local Docker setup.

## 3. Live against your own HA

For maintainers with an HA instance, when you need to exercise the interactive setup wizard, the NOVA owner console, or the OPAQUE + TLS device pairing on real hardware. **Two hard rules:** deploy an *isolated* test add-on (never your production one), and run an *isolated* CLI (never your real config/keyring). Both are covered below.

### Deploy an isolated test add-on

`scripts/dev/ha-app-bootstrap.sh` syncs `nova/` to your HA over SSH and installs it as a local add-on. Give it a **distinct slug and ports** so it can never collide with your production NOVA Relay:

```bash
HA_HOST=<ip> HA_SSH_KEY=~/.ssh/<key> RELAY_AUTH_TOKEN=<token> \
APP_SLUG=ha_nova_relay_test \
  bash scripts/dev/ha-app-bootstrap.sh
```

`HA_HOST`, `HA_SSH_KEY`, `RELAY_AUTH_TOKEN` are required; `SSH_USER`/`SSH_PORT`/`APP_SLUG`/`SUPERVISOR_SLUG` are optional (also read from `.env` / `.env.local`). Point the test overlay's `config.yaml` at unused host ports (e.g. `18791`/`18792`) so the two instances never overlap. `scripts/smoke/ha-app-e2e.mjs` runs a Node smoke pass against the deployed app.

Deploy note: when the source `config.yaml` version drifts from the installed version, `ha apps rebuild` becomes a silent no-op — use `ha store reload` + `ha apps update <slug>`. Never `docker restart` an ingress add-on from outside (its IP changes and HA's ingress route breaks); only `ha apps …` commands.

### Run an isolated CLI

Point the CLI at a throwaway `HOME` and secret store so it can never read or overwrite your real setup:

```bash
HOME=/tmp/nova-test-home \
HA_NOVA_DEV_ROOT="$PWD" \
HA_NOVA_TEST_SECRET_DIR=/tmp/nova-test-secrets \
HA_NOVA_NO_BROWSER=1 \
  ./ha-nova setup claude --relay-url http://<ip>:18791
```

Then pair (`ha-nova pair --relay-url … --code NNNNNN`), exercise skills over the device transport (`ha-nova relay health|core|ws|trace …`), and finish with `ha-nova uninstall --purge --yes` to verify the server-side revoke and local cleanup.

### Isolation env vars

| Variable | Effect | Use for |
| --- | --- | --- |
| `HA_NOVA_DEV_ROOT=<repo>` | Runs the CLI in dev mode — uses the repo's local skills/bundle instead of a released bundle | Any local build |
| `HA_NOVA_TEST_SECRET_DIR=<dir>` | Device-credential slots become `0600` files under `<dir>` instead of the OS keyring | Isolate device pairing from your real keyring |
| `HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` + `HA_NOVA_TEST_KEYRING_FILE=<path>` | The legacy relay-auth token is stored in a file instead of the OS keyring | Isolate the token slot on a desktop |
| `HA_NOVA_KEYRING_SERVICE=<name>` | Overrides the relay-token keyring service name | Isolate the real token slot by name |
| `HA_NOVA_NO_BROWSER=1` | `setup` never opens a browser | Scripted / headless runs |
| `HA_NOVA_NO_UPDATE_NUDGE=1` | Suppresses the background update check | Deterministic test output |

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

Cross-compile for every target with the same `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build` from `cli/` (windows/darwin/linux × amd64/arm64). `scripts/smoke/linux-headless-setup-check.sh` captures the remote Secret Service preflight over SSH for a Linux release proof.

## Safety rules

These are non-negotiable when testing on real machines:

- **Never touch the production add-on, keyring, or credentials.** Use a distinct slug/ports (add-on) and a throwaway `HOME` + `HA_NOVA_TEST_SECRET_DIR` (CLI).
- **Only create your own test objects** on a live HA (helpers, automations you made); leave existing objects read-only.
- **Clean up afterwards** — `ha apps uninstall <test-slug>`, remove the container, and run a leftover scan (0 test objects). Never leave a test add-on running on someone's HA.
- **A green CI/host-safe run is not a release proof.** Releasing still needs the live checks above on the exact commit being tagged (see [docs/releasing.md](../releasing.md)).
