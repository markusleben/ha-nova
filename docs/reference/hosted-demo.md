# HA NOVA hosted demo (demo.hanova.app)

"Try before you connect": the test bed house (`scripts/e2e/testbed/`) on one
public AWS Lightsail instance. Every visitor is a Home Assistant **admin**,
and the house is rebuilt from scratch **every hour at :00 UTC**. First
provisioned on 2026-10-16 (results below); the site needs the DNS record
(Run it, step 2) before it serves anything. Files live in
`scripts/demo/hosted/` (its parent `scripts/demo/` is the unrelated
video-recording kit, `docs/reference/demo-recording.md`).

| File | Runs on | What it does |
| --- | --- | --- |
| `provision.sh` | your Mac | Lightsail instance (IPv4 only); firewall + IMDSv2 applied right after the create call and re-applied on every run; static IPv4; budget alert. Prints every `aws` call; executes only with `--yes`. Idempotent. |
| `teardown.sh` | your Mac | `--keep-ip`: delete the instance (rebuild). `--dns-removed`: instance, static IP, budget. Prints; `--yes` executes. |
| `aws-common.sh` | your Mac | Sourced by both: every call is `--profile personal --region eu-central-1`, account `018368605240` is checked first, ambient AWS env vars are unset. |
| `bootstrap.sh` | host, once per ref | Refuses unless the clone is at `--ref`. Swap, SSH hardening, fail2ban, unattended upgrades, Docker (official repo, key pinned), Node + cosign (checksums pinned), 6 Mbit/s egress cap, internal network, Caddy (validated, recreated), the relay image for this ref, the env (only after that image exists), the first gated reset, then the timers. Re-runs pause the timers until done (restarted on failure too). |
| `reset.sh` | host, hourly + 2 min after boot | Under the lock: hide the house (before any check that can fail), `down.sh`, restore the clone to the pinned sha, restart Caddy if the name points here but nothing is served over TLS yet, `up.sh` with this overlay and the verified HA digest, relay, gates, dashboard, `connect.json`, expose, public gate. |
| `guard.sh` | host, every minute | Skips while a reset holds the lock. Disk/swap watchdog; `demo.mjs guard` (30 s cap); relay round trip (revoked token: reset at once; other failures: after 3 minutes); TLS that came up after a skipped public gate. Anything wrong → full reset. |
| `update-ha.sh` | host, nightly | Latest stable HA: resolve digest, verify the Sigstore signature, refuse downgrades, candidate reset + gates, promote or roll back, prune old images. |
| `host-lib.sh` | host | Shared gates: relay round trip, egress/DNS, listeners, public site; Caddy (re)creation; pinned Node/Caddy images; the lock. |
| `demo.mjs` | host | `setup` (seeded http config check, local-only owner, admin `demo/demo`, drop onboarding extras), `token` (the relay user's LLAT), `dashboard` (also shows the "Demo add-on" device as "Demo app"), `guard`, `smoke`. |
| `edge.yml`, `Caddyfile`, `site/` | host | Caddy in its own compose project: TLS, CSP, blocked paths, landing redirect, restarting page (`site/icon.svg`: a copy of `assets/icon.svg`, kept equal by `tests/config/hosted-demo-icon.test.ts`), `/relay/*`, `/connect.json`, `/abuse`, privacy-filtered access log. |
| `docker-compose.yml` | host | Overlay over the test bed compose: internal network only, DNS sink, sealed directories, limits, relay settings. |
| `mosquitto.conf`, `mosquitto.acl` | host | Broker split: seeding listener (container loopback) vs. Home Assistant listener (no discovery writes). |
| `config/` | host | Over the test bed config: `configuration.yaml`, `packages/demo.yaml` (reset sensor + :55 warning), `packages/commands.yaml` (masks command_line + rest). |

Test bed hooks (`up.sh`/`down.sh`): `TESTBED_OVERLAY=<this dir>` (merge
compose, copy config, HA + broker only, skip the port probe, run
`demo.mjs setup`), `TESTBED_OWNER_PASSWORD`, and `TESTBED_WIPE_IMAGE` (down.sh
wipes `.state` with Caddy's pinned image, which prunes never remove). Local
runs set none of them. The
relay's `nova/Dockerfile.standalone` takes an optional `NODE_IMAGE` build arg
(default `node:22-alpine`, unchanged); the demo passes a digest-pinned tag.

## Run it

```bash
# 1. Dry run (no AWS call at all), then for real. --ref: a 40-hex merge commit on GitHub containing this dir.
bash scripts/demo/hosted/provision.sh --ref <sha> --alert-email <you> --abuse-contact <mail-or-url> [--ssh-cidr <ip>/32]
bash scripts/demo/hosted/provision.sh --ref <sha> --alert-email <you> --abuse-contact <mail-or-url> --yes
# 2. Namecheap: A demo.hanova.app -> printed static IPv4 (no AAAA: IPv4 only).
#    Until then nothing is served over TLS, every reset skips the public
#    gate (journalctl -u ha-nova-demo-reset), and HA updates wait. The first
#    reset after the name resolves to this instance (next :00, or the one
#    after while resolvers still cache "no such name") restarts Caddy before
#    it rebuilds the house, so the gate usually runs in that reset; if TLS
#    comes later, the guard starts one reset. Skip the wait:
#    sudo docker restart ha-nova-demo-caddy
# 3. Lightsail browser SSH: sudo tail -f /var/log/cloud-init-output.log   (~10 min),
#    then journalctl -fu ha-nova-demo-reset -u ha-nova-demo-update
# 4. https://demo.hanova.app -> hanova.app/demo/ -> Open the demo, login demo / demo
```

Rebuild from scratch (new ref, or incident response):
`teardown.sh --keep-ip --yes && provision.sh … --yes` (same IPv4, DNS untouched).

## Isolation model

One shared account for everyone: `demo` / `demo`, **admin** (not owner).
Visitors can try everything in Home Assistant and in ha-nova, including
writes, undo, and deletes. Nobody's changes are private and nothing survives
the next full hour.

- **Can:** everything an HA admin can in the UI and APIs: automations,
  scripts, scenes, helpers, dashboards, users, integrations that need no
  internet, service calls, the public relay (below).
- **Cannot** (verified on 2026.10.1): reach the internet from HA or the relay;
  create, upload, or restore backups (empty read-only `/config/backups`;
  Caddy 403s `/api/backup/*`); change what HA loads (`configuration.yaml`,
  `packages/` read-only; no `custom_components`/`python_scripts`); write any
  file a service can target (`camera.snapshot`/`notify.file` paths must be in
  `www` or `/media`, both empty read-only); host files publicly (`/local/*`,
  `/api/tts_proxy/*` 403); add or delete MQTT discovery entities
  (`mqtt.publish` reaches only device command topics); load scripts, images,
  or frames from another origin in anyone's browser (CSP); reach onboarding
  (`/api/onboarding*`, `/onboarding.html` 403 publicly); delete or
  deactivate the owner (HA refuses); keep a changed `demo` password (reset
  within a minute); keep anything past :00.
- **The relay's user** (`nova-relay`, admin, random password per reset) owns
  the token behind the public relay, so it never shows up in the `demo`
  profile; deleting it, or revoking its token, triggers a full reset.
- **The owner** (`testbed`) is host tooling only: random password per reset
  (`/root/ha-nova-demo-owner-password`), `local_only`, used through its
  long-lived token on `127.0.0.1:18123`. Maintainer UI access: SSH tunnel
  `-L 18123:127.0.0.1:18123`, then http://127.0.0.1:18123.
- **Real testing** (private data, write/undo with nobody else around) belongs
  on the private local test bed: `npm run testbed:up`. A friendlier
  `ha-nova demo` wrapper for that is planned.

## Reset schedule and visibility

- Full rebuild every hour at :00 UTC and 2 minutes after a reboot (the
  broker keeps no state) (`ha-nova-demo-reset.timer`); measured 32-108 s
  locally, estimated 1-3 min on the 1 GB host. The guard also triggers it on
  demand. `reset.sh`, `guard.sh`, and `update-ha.sh` share one lock.
- The house is exposed only after every gate passed: `reset.sh` removes
  `/var/lib/ha-nova-demo/public/ready` first and touches it last. Until then,
  and whenever HA is gone or answers 502/503/504, Caddy serves "Demo is
  restarting" (503, auto-refresh 15 s, link to `/start`; styled like
  hanova.app, its star `/_demo/icon.svg` is served while down); `/relay/*` answers
  `{"error":"demo_resetting"}` (503) when the relay is gone or the house not
  ready, while the relay's own JSON errors (it uses 502 for upstream
  failures) pass through.
- Gates in every reset (a failure takes the house down again and logs
  `journalctl -t ha-nova-demo-reset -p err`): relay round trip (`/core` GET
  `/api/`, `/ws` get_states, `/health` connected), egress + DNS (4 s probes
  in parallel: external name and IP from HA and the relay must fail, while
  the same tools must reach house targets and service names must resolve),
  listeners, then the public site through Caddy's TLS on the host (`/`
  redirects to the landing page, `/?auth_callback=1` reaches HA,
  `/connect.json` with its CORS header, `/_demo/icon.svg`, `/relay/health`
  with the public token, the login page,
  403 on one path per blocked pattern, read from the Caddyfile); skipped
  only while nothing at all is served over TLS (anything served, even with
  an expired or untrusted certificate, must pass). This also re-checks the
  sandbox after every dockerd upgrade.
- Landing page: `https://hanova.app/demo/` (`demo/index.html` in
  `markusleben/ha-nova-site`, the site's design). `/` without a query and
  `/start` answer 302 there; HA's own redirects to `/` always carry a query
  (sign-in returns to `/?auth_callback=1&code=...`) and still reach HA. It
  shows the personal-data warning above the buttons, login, admin rights,
  the next reset, the running HA version (read from `/connect.json`, which
  answers GETs with `Access-Control-Allow-Origin: https://hanova.app`), a
  link to `/connect.json`, the abuse link, and "Open the demo"
  (`/demo-house/house`). It promises nothing unreleased and stays unlinked
  from the site (`noindex`) until the demo ships in a release. Deploy this
  Caddyfile only once that page is live (`bootstrap.sh` refuses otherwise). Verified locally (throwaway HA
  2026.10.1 behind this Caddyfile): logged-out `/` and `/start`, sign-in
  back to a deep link and to `/?auth_callback=1`, logout (HA goes to `/`)
  lands on the landing page.
- In HA: `sensor.demo_next_reset` (UTC timestamp; agents:
  `ha-nova get sensor.demo_next_reset`), the `demo-house` dashboard as the
  system-wide default with a banner as every view's header (the personal-data
  warning first, minutes to the reset, HA version, links to `/start` and
  `/abuse`), and a notice at :55 UTC (with the same warning).
  Both use `utcnow()`, because visitors can change HA's time zone.
- The guard restores the banner dashboard and the default panel within a
  minute; anything else comes back at :00.

## Connecting an AI client

`https://demo.hanova.app/connect.json` (rewritten every reset, never in the
repo): `ha_url`, `ha_version`, `relay_url` (`https://demo.hanova.app/relay`),
`relay_token`, the login, `next_reset`. The relay image is built once per
ref by `bootstrap.sh` (digest-pinned Node base, keep label); resets never
build or pull it. The relay token is generated once
(`/var/lib/ha-nova-demo/relay-token`, root 0600) and kept across resets, so
agents keep working; only the HA token behind it rotates. The relay runs as
the `nova-relay` user (the owner's token never leaves the host), `/files` is
off (verified: `FILE_ACCESS_DISABLED`), its snapshot store `/backups` is an
8 MB tmpfs, it has no egress, and Caddy 403s `/relay/pair*` and `/relay/home*`.
The CLI appends `/ws`, `/core`, `/health` to the base URL, so a path prefix
works for calls; `ha-nova setup`'s URL validation with a path is unverified
(open item 1).

## Threat model + hardening

Assumed attacker: anyone on the internet with admin rights in this HA and
this repository. Denied goals: code execution outside HA's own sandbox, the
host or demo as an attack platform (outbound traffic, hosting files or
scripts for third parties), the AWS account, persistence past :00.

| Layer | Control |
| --- | --- |
| Network | IPv4-only instance (Docker's userland proxy would hand every IPv6 visitor to HA as one bridge address). Lightsail firewall replaced as a whole right after the create call (retried until the API accepts it for the pending instance; every re-run converges it again, so an aborted run leaves nothing open for long): 80/443 tcp + 443 udp public, 22 tcp only for `lightsail-connect` (+ one `--ssh-cidr`); `provision.sh` then re-reads the port states and IMDS options and fails on any other open port, a public 22, or optional IMDS tokens. Every reset fails on any listener beyond loopback other than 22/80/443. Only Caddy publishes ports; its tooling port binds 127.0.0.1. |
| Egress | HA, broker, relay sit only on `ha-nova-demo-house` (`internal: true`, no route out). Linux dockerd's embedded resolver can still forward external names from internal networks (a DNS exfiltration channel), so all three use `dns: 192.0.2.1` (TEST-NET-1, unroutable): service names resolve, external ones time out. Every reset (bootstrap's first included) fails if an external name or address is reachable from HA or the relay, or if the probes cannot reach house targets (a missing tool must not pass). Only Caddy (ACME) and dockerd (pulls) reach the internet. Egress capped at 6 Mbit/s (`tc tbf`), ~2 TB/month. |
| SSH / OS | Key-only, no root, no passwords, no forwarding of agents/X11; fail2ban. Ubuntu 24.04, unattended upgrades (Ubuntu security + Docker repo), reboot 04:30 UTC when needed. Docker from download.docker.com (one key, fingerprint pinned), `live-restore`, daemon-wide `no-new-privileges`, unix socket only. |
| Containers | All: `no-new-privileges`, `cap_drop: [ALL]`, read-only root, CPU/memory/pids limits (HA 1.5 CPU, 640 MiB + swap to 1 GiB, 512 pids), size-capped logs, no docker.sock, no host network, nothing privileged, images by digest (HA: verified nightly, below). HA keeps 5 caps for s6-overlay; Caddy uid 1000 + `NET_BIND_SERVICE`; broker uid 1883; relay uid 100. |
| HA config surface | `/config` stays writable (`.storage`, recorder, UI editors' atomic writes), but `configuration.yaml` and `packages/` are read-only binds and `custom_components`, `python_scripts`, `www`, `backups`, `image`, `tts`, `/media` are empty read-only tmpfs mounts. `home-assistant.log` is `/dev/null`; `file_upload` lands in a 64 MiB `/tmp` tmpfs. No `default_config`; onboarding's met/google_translate/radio_browser/shopping_list entries are deleted. No shell_command/command_line/rest/python_script anywhere. go2rtc (can run `exec:` sources) has no config flow, so it cannot be added. |
| MQTT | `mqtt.publish` is an admin service (read in the 2026.10.1 source), so visitors have it. The broker gives HA a listener whose ACL allows reads and only `testbed/+/set`, `testbed/plug/factory_reset`, `testbed/router/restart` writes (verified: discovery deletes/additions dropped, commands pass). Seeding uses a second listener on the broker container's loopback. |
| Browser | CSP: scripts, frames, connections only from this origin (+ `data:`/`blob:`, inline for HA's own boot); images from this origin and `https://brands.home-assistant.io`. HA 2026.10 serves integration icons at `/api/brands/integration/<domain>/<file>` by fetching that CDN server-side, which the house cannot; Caddy answers those paths (a `[a-z0-9_]` domain and HA's eight icon/logo file names only, no query kept) with a 302 to the same public image, so the visitor's browser fetches it and the house still has no egress. Map tiles (`/api/map_tiles/`, e.g. the Zones page map) are proxied the same way and stay blank on purpose: the map library fetches them with `fetch()`, so a redirect would also widen `connect-src`, and it would skip the caching HA's proxy does for OpenStreetMap's tile usage policy. Verified: an external dashboard resource and external markdown images are blocked; the HA UI works (dashboards, settings, automations, history, energy, media). HA's markdown sanitizer strips `onerror`, `javascript:` links, `<script>` (verified). HSTS, `nosniff`, `X-Frame-Options DENY`, `frame-ancestors 'none'`, 1 MB request bodies, Caddy admin API off. |
| Accounts | Guard every minute via the owner's token (WS handshake deadline 15 s, whole check 30 s): owner active + local-only, `demo` and `nova-relay` checked by the user ids recorded at creation (same username, active, admin, not local-only; a deleted and re-created look-alike fails), http config (`use_x_forwarded_for`, no IP bans, trusted proxy, nothing pending, running the stable slot) unchanged, relay not reporting an `auth` disconnect, else full reset. Repairs: the `demo` password; failed-login notices (visitor IPs) and notices that look like links (`//`, markdown links and references, `href`, `www.`) dismissed; banner dashboard and default panel restored. HA refuses to delete or deactivate the owner (verified). |
| Proxy trust | `trusted_proxies` = Caddy's fixed 10.89.240.2 only (dynamic addresses start at .128). HA 2026.10 deprecated the `http:` YAML (a repair, plus a 5-minute trial to promote), so there is none: `config/.storage/http` seeds HA's own store (stable slot, YAML migration marked done) before the first boot, every reset; `demo.mjs setup` fails loud if HA runs anything else. **No IP bans:** HA bans on any 401, the password is public, the owner cannot log in from outside anyway; bans would only lock visitors (or the tooling path) out. |
| Secrets | None in the repo or user-data (readable via instance metadata). Owner password and token, relay user password and token: generated on the host per reset; the public relay token once. IMDSv2 only, hop limit 1. |
| AWS | No role or credentials of the demo's account on the instance. Lightsail's own platform role (`AmazonLightsailInstanceRole`, instance profile in an AWS-owned account) is on the IMDS for host root only: IMDSv2, hop limit 1, and the house network has no route to it (verified from every container). Budget alarm at 10 USD. |
| Logs / privacy | Caddy access log: 7 days, client IPs truncated to /24 (/48), no headers, no `authSig`/`token`/`code` query values. `/abuse` shows the contact given at provisioning. Visitors' browsers fetch integration icons from `brands.home-assistant.io` (their IP, and the demo's origin as referrer); `/` and `/start` redirect to the landing page on hanova.app (GitHub Pages), which in turn fetches `/connect.json`; no other third party. |
| Integrity | Hourly rebuild restores the clone (`checkout --force`, `clean -ffdx`) and all HA state. Caddy is validated with the pinned image and recreated whenever `edge.yml`, the Caddyfile, or `site/` change (hash compare in every reset; `bootstrap.sh` always). Host compromise is not fixed by a reset: rebuild the instance. |

**Detect:** budget mail; `journalctl -u ha-nova-demo-guard -u ha-nova-demo-reset`
(frequent guard resets = someone breaking things); `docker stats`, `free -m`,
`df -h`; `ss -ltn`; Caddy access log in `/var/lib/ha-nova-demo/logs`, ACME and
TLS in `docker logs ha-nova-demo-caddy`. An expired or untrusted certificate
fails the public gate and keeps the house down (rebuilt every minute) until
TLS works again. **Respond:**
rebuild (`teardown.sh --keep-ip --yes && provision.sh … --yes`); rotate the
Lightsail key pair if SSH was involved.

### Verified empirically vs. read in code

Empirically, on Docker Desktop (macOS) with a throwaway copy (own compose
projects, names, ports; HA 2026.10.1 from `ghcr.io` by digest): full resets
(24-63 s; Caddy kept serving the 503 page; relay token unchanged across
resets), 156 entities / 6 MQTT; `demo.mjs smoke` passes; no route out from
HA, relay, broker; external DNS fails after the 10 s sink timeout while
`mosquitto`/`homeassistant` resolve; public 403 on onboarding, backup, `/local`,
TTS, relay pairing paths while the tooling port still onboards; read-only
mounts; `/dev/null` log; backup create/restore fail; `camera.snapshot`
refused/RO; MQTT ACL; owner delete/deactivate refused; guard catches owner,
relay-user, and http changes, repairs the `demo` password, restores a deleted
banner dashboard and an overridden default panel (`frontend` system data
`core.default_panel`), dismisses a link notice and keeps a plain one; relay
reports `ha_ws_disconnect_reason: "auth"` once its user is deleted and it
reconnects; relay `/files` off, 8 MB store; CSP in a real browser (external
script and images blocked, UI works); login and logout redirects (the
landing redirect re-checked locally after the move to hanova.app); no 401s from admin UI browsing; masked access log; CPU and pids
limits. Gated reset sequence (driver with `host-lib.sh` sourced): house and
relay hidden until `ready` (503 page / `demo_resetting` JSON, landing
redirect still served), relay round trip with the prebuilt image (built from the digest-pinned
Node base, keep label set), parallel egress gate in 4 s, public gate; an
upstream answering 502, 503, or 504 and a missing upstream both get the
restarting page, while the relay's own 502 JSON passes through; a paused HA
fails `demo.mjs guard` after 15 s instead of hanging. Signature: `cosign verify` (v3.1.3) of
`ghcr.io/home-assistant/home-assistant:2026.10.1` passes with the pinned
identity + issuer; a different tag's identity and a foreign digest fail.

On the real host (2026-10-16, Lightsail `micro_3_0`, before DNS): `provision.sh`
and every `aws` call; the launch script under dash; `bootstrap.sh` (about 3.5 min
from create to the first gated build); systemd timers (the hourly reset at :00
rebuilt in about 70 s and wiped a visitor's automation, helper state, user, and
integration); `update-ha.sh` (resolve, cosign verify under systemd, "already
on"); the guard (restored a visitor-changed `demo` password in 57 s, reset on an
owner change in 34 s); `tc`, `flock`, `ss`, listener and egress gates on Linux
dockerd (no route from HA, relay, broker; external DNS, IP, IPv6, IMDS fail; the
probes reach house targets); limits enforced (a fork past the broker's pids
limit fails); backup creation fails (read-only `/config/backups`, no other
agent). From outside, with Caddy's internal CA swapped in for the test: only
80/443 open (22 only for `lightsail-connect`), headers, every blocked path and
variant 403, relay token and `/files` off, `demo` login, owner login refused
with the right password and spoofed forwarding headers ("User cannot
authenticate remotely"); an untrusted certificate fails the public gate and
takes the house down. Not yet run: the public gate with a Let's Encrypt
certificate (needs the DNS record; then it runs on every reset).

Read in code only: ban middleware (any 401 counts; login notices always),
allowlist defaults (`www` + media dirs always added), `mqtt.publish`
admin-only, go2rtc/local_file/file manifests and checks, HA 2026.10 sending a
`lovelace` panel without a dashboard entry to the Home panel.

### Residual risks (accepted for a simulated, hourly-reset house)

- Shared admin: anyone can break the house for everyone until :00, or
  trigger guard resets repeatedly (a 1-3 min outage each).
- Content written by one visitor (notifications with links, entity names,
  dashboards, todo items, LLATs they minted) is visible to others until :00.
  External links are allowed (CSP does not govern navigation).
- `local_file` (UI config flow, no path check) can show any file the HA
  container can read as a camera image, e.g. `.storage/auth`, which allows
  forging owner tokens: owner-equivalent API access until the next reset
  (bypasses the owner's local-only login). Nothing from the host or the relay
  is mounted.
- A per-user default dashboard (`frontend` user data) set by one visitor
  applies to everyone on `demo` until :00; the guard only enforces the
  system-wide one.
- No rate limiting (Caddy core has none; `caddy list-modules` on 2.11.4). A
  flood or heavy templates/automations can stall HA; bounded by HA's CPU and
  memory limits (the host and guard stay responsive) and the hourly reset.
- Disk: recorder (`purge_keep_days: 1`), `known_devices.yaml`, `.storage`
  (dashboards, user data, todo lists), `profiler` dumps can grow within an
  hour; the guard resets at 80 % disk or 80 % swap.
- Transfer: Lightsail also counts inbound traffic toward the 2 TB; only
  egress is shaped.
- HA raises a `yaml_still_present_after_migration` repair (http YAML breaks
  in 2027.2): before that release, configure http through `http/config/configure`.

## Cost (eu-central-1, list prices)

| Item | USD / month |
| --- | --- |
| Lightsail `micro_3_0` (1 GB RAM, 2 vCPU burst, 40 GB SSD, 2 TB transfer, public IPv4) | 7.00 |
| Static IPv4 while attached | 0.00 (detached: ~0.005/h) |
| AWS Budgets (one budget, no actions) | 0.00 |
| Transfer above 2 TB | ~0.09/GB; egress capped by `tc` at ~2 TB |
| **Total** | **7.00 + transfer above 2 TB** (+ 19 % VAT on a German account: ~8.33) |

Budget `ha-nova-demo-monthly`: 10 USD, account-wide, mail at 80 % actual and
100 % forecast.

## Memory (1 GB RAM)

Measured with every limit: HA 270-310 MiB, Caddy ~29 MiB, relay ~25 MiB,
broker ~3 MiB. Estimated host total with Ubuntu (~150-200 MiB) and Docker
(~80-100 MiB): ~600-750 MiB; the 1 GB swapfile absorbs reset peaks (the
relay image build is cached after the first reset).

## Home Assistant updates (latest stable, by verified digest)

Nightly at 03:10 UTC, `update-ha.sh`:
1. reads `homeassistant.default` from `https://version.home-assistant.io/stable.json`
   (must match `YYYY.M.P`);
2. resolves `ghcr.io/home-assistant/home-assistant:<version>` to its index digest;
3. `cosign verify` (v3.1.3, checksum-pinned) of that digest, keyless:
   identity `https://github.com/home-assistant/core/.github/workflows/builder.yml@refs/tags/<version>`,
   issuer `https://token.actions.githubusercontent.com`, transparency log on;
4. if the digest is new: a full reset with it as candidate, passed to
   `reset.sh --candidate` directly (a timer reset waiting on the lock cannot
   take it; Caddy shows the restarting page), with every reset gate; then the
   running container must use exactly that digest, `demo.mjs smoke` must pass
   (house complete, a light round-trips, every guard invariant, reported
   version = expected), the relay must round-trip, and the public gate must
   pass (under the lock; while nothing is served over TLS, no candidate
   is tried at all);
5. pass: the digest becomes `/var/lib/ha-nova-demo/ha-image` (root 0600) for
   every hourly reset and the previous image is removed; fail: a reset with
   the last good digest, the candidate image removed, and the reason in the
   journal (`journalctl -t ha-nova-demo-update -p err`).

A stable version older than the running one is refused (`sort -V`); going
back needs an explicit `--pin`. Every run ends with `docker image prune -af
--filter until=48h --filter label!=ha-nova-demo.keep=1`, but only while HA
is running the good digest: an image in use is never pruned, and the relay
image (keep label) survives even while the relay is down.

`reset.sh` refuses to run without a verified `ghcr.io/...@sha256` reference;
compose never falls back to a tag. Bootstrap seeds the file from the test
bed's pinned version (`update-ha.sh --pin 2026.10.1`, signature-checked).
The full test bed `verify.sh` is not the gate: it needs the Go CLI and the
command_line/rest entities the demo masks; `smoke` covers what the demo
relies on. Manual rollback: `update-ha.sh --pin <older version>` (2026.10 or
newer; `--pin` refuses older releases, which cannot read the seeded
`.storage/http`), then
`systemctl start ha-nova-demo-reset`. A release that changes what the demo
depends on (e.g. the seeded `.storage/http` format) fails its smoke
test and stays out until the repo is fixed.

## Bumping other pins

Mosquitto (test bed compose tag, overlay digest), Caddy (`edge.yml` and
`host-lib.sh`), alpine (`down.sh`), Node for the relay build (`host-lib.sh`),
Node and cosign on the host (`bootstrap.sh`, version + SHA256). Then rebuild
with the new sha. When a release raises `min_relay_version`, the demo's
relay (built from the provisioned ref) is too old for that CLI: re-provision
with the release's merge commit (`docs/releasing.md`).

## Open items and questions

1. CLI: done as `ha-nova try` in 0.27 (`docs/work/2026-10-18-onboarding-0.27-spec.md`
   §8); the public page names it only once that release ships.
2. Abuse contact to publish (`--abuse-contact`): a mail address or a URL?
3. Rate limiting: accept none, or a custom Caddy build (`caddy-ratelimit`)
   or CloudFront + WAF (cost)?
4. SSH: `lightsail-connect` only, or also your IP via `--ssh-cidr`?
5. Budget alert address; is account-wide the right scope?
