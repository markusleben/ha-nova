# HA NOVA hosted demo (demo.hanova.app)

"Try before you connect": the test bed house (`scripts/e2e/testbed/`) on one
public AWS Lightsail instance. Every visitor is a Home Assistant **admin**,
and the house is rebuilt from scratch **every hour at :00 UTC**. Prepared, not
deployed: nothing here has run against AWS yet.
(The parent `scripts/demo/` is the unrelated video-recording kit,
`docs/reference/demo-recording.md`.)

| File | Runs on | What it does |
| --- | --- | --- |
| `provision.sh` | your Mac | Lightsail instance (IPv4 only), static IPv4, firewall, IMDSv2, budget alert. Prints every `aws` call; executes only with `--yes`. Idempotent. |
| `teardown.sh` | your Mac | `--keep-ip`: delete the instance (rebuild). `--dns-removed`: instance, static IP, budget. Prints; `--yes` executes. |
| `aws-common.sh` | your Mac | Sourced by both: every call is `--profile personal --region eu-central-1`, account `018368605240` is checked first, ambient AWS env vars are unset. |
| `bootstrap.sh` | host, once | Swap, SSH hardening, fail2ban, unattended upgrades, Docker (official repo, key pinned), Node + cosign (checksums pinned), 6 Mbit/s egress cap, internal network, Caddy, timers, first build, listener + egress/DNS gates. |
| `reset.sh` | host, hourly | `down.sh`, restore the clone to the pinned sha, `up.sh` with this overlay and the verified HA digest, relay (stable public token, fresh HA token), dashboard, `connect.json`. |
| `guard.sh` | host, every minute | Disk/swap watchdog; `demo.mjs guard` (30 s cap); relay health incl. a revoked HA token. Anything wrong → full reset. |
| `update-ha.sh` | host, nightly | Latest stable HA: resolve digest, verify the Sigstore signature, candidate boot + smoke, promote or roll back. |
| `demo.mjs` | host | `setup` (http trial promote, local-only owner, admin `demo/demo`, drop onboarding extras), `token` (the relay user's LLAT), `dashboard`, `guard`, `smoke`. |
| `edge.yml`, `Caddyfile`, `site/` | host | Caddy in its own compose project: TLS, CSP, blocked paths, landing page, restarting page, `/relay/*`, `/connect.json`, `/abuse`, privacy-filtered access log. |
| `docker-compose.yml` | host | Overlay over the test bed compose: internal network only, DNS sink, sealed directories, limits, relay settings. |
| `mosquitto.conf`, `mosquitto.acl` | host | Broker split: seeding listener (container loopback) vs. Home Assistant listener (no discovery writes). |
| `config/` | host | Over the test bed config: `configuration.yaml`, `packages/demo.yaml` (reset sensor + :55 warning), `packages/commands.yaml` (masks command_line + rest). |

Test bed hooks (`up.sh`/`down.sh`): `TESTBED_OVERLAY=<this dir>` (merge
compose, copy config, HA + broker only, skip the port probe, run
`demo.mjs setup`) and `TESTBED_OWNER_PASSWORD`. Local runs set neither.

## Run it

```bash
# 1. Dry run (no AWS call at all), then for real. --ref: a 40-hex merge commit on GitHub containing this dir.
bash scripts/demo/hosted/provision.sh --ref <sha> --alert-email <you> --abuse-contact <mail-or-url> [--ssh-cidr <ip>/32]
bash scripts/demo/hosted/provision.sh --ref <sha> --alert-email <you> --abuse-contact <mail-or-url> --yes
# 2. Namecheap: A demo.hanova.app -> printed static IPv4 (no AAAA: IPv4 only).
# 3. Lightsail browser SSH: sudo tail -f /var/log/cloud-init-output.log   (~10 min)
# 4. https://demo.hanova.app, login demo / demo
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

- Full rebuild every hour at :00 UTC (`ha-nova-demo-reset.timer`); measured
  34-48 s locally (cached images), estimated 1-3 min on the 1 GB host.
  The guard also triggers it on demand.
- During the rebuild Caddy serves "Demo is restarting" (503, auto-refresh
  15 s, link to `/start`); `/relay/*` answers `{"error":"demo_resetting"}`
  (503). For ~20 s after HA answers, the house is still being seeded.
- Landing page: `/` (without a query; HA's own redirects always carry one,
  e.g. `?auth_callback=1`) and `/start`: login, next reset, the running HA
  version, "no personal data", "connect your AI", abuse link, "Open the demo"
  (`/demo-house/house`). Verified: login redirect, logout redirect.
- In HA: `sensor.demo_next_reset` (UTC timestamp; agents:
  `ha-nova get sensor.demo_next_reset`), the `demo-house` dashboard as the
  system-wide default with a banner as every view's header (minutes to the
  reset, HA version, links to `/start` and `/abuse`), and a notice at :55 UTC.
  Both use `utcnow()`, because visitors can change HA's time zone.
- The guard restores the banner dashboard and the default panel within a
  minute; anything else comes back at :00.

## Connecting an AI client

`https://demo.hanova.app/connect.json` (rewritten every reset, never in the
repo): `ha_url`, `ha_version`, `relay_url` (`https://demo.hanova.app/relay`),
`relay_token`, the login, `next_reset`. The relay token is generated once
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
| Network | IPv4-only instance (Docker's userland proxy would hand every IPv6 visitor to HA as one bridge address). Lightsail firewall replaced as a whole: 80/443 tcp + 443 udp public, 22 tcp only for `lightsail-connect` (+ one `--ssh-cidr`); `provision.sh` re-reads the port states and fails on any other open port or a public 22. `bootstrap.sh` fails on any listener beyond loopback other than 22/80/443. Only Caddy publishes ports; its tooling port binds 127.0.0.1. |
| Egress | HA, broker, relay sit only on `ha-nova-demo-house` (`internal: true`, no route out). Linux dockerd's embedded resolver can still forward external names from internal networks (a DNS exfiltration channel), so all three use `dns: 192.0.2.1` (TEST-NET-1, unroutable): service names resolve, external ones time out. `bootstrap.sh` refuses to finish if `getent hosts example.com` succeeds in HA or a lookup succeeds in the relay. Only Caddy (ACME) and dockerd (pulls) reach the internet. Egress capped at 6 Mbit/s (`tc tbf`), ~2 TB/month. |
| SSH / OS | Key-only, no root, no passwords, no forwarding of agents/X11; fail2ban. Ubuntu 24.04, unattended upgrades (Ubuntu security + Docker repo), reboot 04:30 UTC when needed. Docker from download.docker.com (one key, fingerprint pinned), `live-restore`, daemon-wide `no-new-privileges`, unix socket only. |
| Containers | All: `no-new-privileges`, `cap_drop: [ALL]`, read-only root, CPU/memory/pids limits (HA 1.5 CPU, 640 MiB + swap to 1 GiB, 512 pids), size-capped logs, no docker.sock, no host network, nothing privileged, images by digest (HA: verified nightly, below). HA keeps 5 caps for s6-overlay; Caddy uid 1000 + `NET_BIND_SERVICE`; broker uid 1883; relay uid 100. |
| HA config surface | `/config` stays writable (`.storage`, recorder, UI editors' atomic writes), but `configuration.yaml` and `packages/` are read-only binds and `custom_components`, `python_scripts`, `www`, `backups`, `image`, `tts`, `/media` are empty read-only tmpfs mounts. `home-assistant.log` is `/dev/null`; `file_upload` lands in a 64 MiB `/tmp` tmpfs. No `default_config`; onboarding's met/google_translate/radio_browser/shopping_list entries are deleted. No shell_command/command_line/rest/python_script anywhere. go2rtc (can run `exec:` sources) has no config flow, so it cannot be added. |
| MQTT | `mqtt.publish` is an admin service (read in the 2026.10.1 source), so visitors have it. The broker gives HA a listener whose ACL allows reads and only `testbed/+/set`, `testbed/plug/factory_reset`, `testbed/router/restart` writes (verified: discovery deletes/additions dropped, commands pass). Seeding uses a second listener on the broker container's loopback. |
| Browser | CSP: scripts, images, frames, connections only from this origin (+ `data:`/`blob:`, inline for HA's own boot). Verified: an external dashboard resource and external markdown images are blocked; the HA UI works (dashboards, settings, automations, history, energy, media). HA's markdown sanitizer strips `onerror`, `javascript:` links, `<script>` (verified). HSTS, `nosniff`, `X-Frame-Options DENY`, `frame-ancestors 'none'`, 1 MB request bodies, Caddy admin API off. |
| Accounts | Guard every minute via the owner's token (WS handshake deadline 15 s, whole check 30 s): owner active + local-only, `demo` and `nova-relay` checked by the user ids recorded at creation (same username, active, admin, not local-only; a deleted and re-created look-alike fails), http config (`use_x_forwarded_for`, no IP bans, trusted proxy, nothing pending) unchanged, relay not reporting an `auth` disconnect, else full reset. Repairs: the `demo` password; failed-login notices (visitor IPs) and notices that look like links (`//`, markdown links and references, `href`, `www.`) dismissed; banner dashboard and default panel restored. HA refuses to delete or deactivate the owner (verified). |
| Proxy trust | `trusted_proxies` = Caddy's fixed 10.89.240.2 only (dynamic addresses start at .128). HA 2026.10 imports the `http:` YAML as a 5-minute trial; `demo.mjs setup` promotes it right after onboarding and fails loud otherwise. **No IP bans:** HA bans on any 401, the password is public, the owner cannot log in from outside anyway; bans would only lock visitors (or the tooling path) out. |
| Secrets | None in the repo or user-data (readable via instance metadata). Owner password and token, relay user password and token: generated on the host per reset; the public relay token once. IMDSv2 only, hop limit 1. |
| AWS | No IAM role or credentials on the instance (Lightsail has none). Budget alarm at 10 USD. |
| Logs / privacy | Caddy access log: 7 days, client IPs truncated to /24 (/48), no headers, no `authSig`/`token`/`code` query values. `/abuse` shows the contact given at provisioning. |
| Integrity | Hourly rebuild restores the clone (`checkout --force`, `clean -ffdx`) and all HA state. Host compromise is not fixed by a reset: rebuild the instance. |

**Detect:** budget mail; `journalctl -u ha-nova-demo-guard -u ha-nova-demo-reset`
(frequent guard resets = someone breaking things); `docker stats`, `free -m`,
`df -h`; `ss -ltn`; Caddy log in `/var/lib/ha-nova-demo/logs`. **Respond:**
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
script and images blocked, UI works); landing page, login and logout
redirects; no 401s from admin UI browsing; masked access log; CPU and pids
limits. Signature: `cosign verify` (v3.1.3) of
`ghcr.io/home-assistant/home-assistant:2026.10.1` passes with the pinned
identity + issuer; a different tag's identity and a foreign digest fail.

Docker Desktop is not Linux dockerd: the DNS sink and the egress gate must be
confirmed on the host (`bootstrap.sh` does, and fails otherwise).

Read in code only: ban middleware (any 401 counts; login notices always),
allowlist defaults (`www` + media dirs always added), `mqtt.publish`
admin-only, go2rtc/local_file/file manifests and checks, HA 2026.10 sending a
`lovelace` panel without a dashboard entry to the Home panel. Not run at all:
`bootstrap.sh`, systemd units, `tc`, `update-ha.sh` and `guard.sh`/`reset.sh`
as such (their steps ran through an equivalent local driver), every `aws` call.

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
4. if the digest is new: a full reset with it as candidate (Caddy shows the
   restarting page), then checks that the running container uses exactly that
   digest and `demo.mjs smoke` (house complete, a light round-trips, every
   guard invariant, reported version = expected);
5. pass: the digest becomes `/var/lib/ha-nova-demo/ha-image` (root 0600) for
   every hourly reset and the previous image is removed; fail: a reset with
   the last good digest, the candidate image removed, and the reason in the
   journal (`journalctl -t ha-nova-demo-update -p err`).

A stable version older than the running one is refused (`sort -V`); going
back needs an explicit `--pin`. Every run ends with `docker image prune -af
--filter until=48h`, but only while HA is running the good digest: an image
in use is never pruned, so the good one always survives.

`reset.sh` refuses to run without a verified `ghcr.io/...@sha256` reference;
compose never falls back to a tag. Bootstrap seeds the file from the test
bed's pinned version (`update-ha.sh --pin 2026.10.1`, signature-checked).
The full test bed `verify.sh` is not the gate: it needs the Go CLI and the
command_line/rest entities the demo masks; `smoke` covers what the demo
relies on. Manual rollback: `update-ha.sh --pin <older version>`, then
`systemctl start ha-nova-demo-reset`. A release that changes what the demo
depends on (e.g. the `http:` YAML import, breaking in 2027.2) fails its smoke
test and stays out until the repo is fixed.

## Bumping other pins

Mosquitto (test bed compose tag, overlay digest), Caddy (`edge.yml`), alpine
(`down.sh`), Node and cosign (`bootstrap.sh`, version + SHA256). Then rebuild
with the new sha.

## Open items and questions

1. CLI (spec only, not implemented): `ha-nova setup <client> --demo` that
   fetches `connect.json`, accepts a relay URL with a path, skips pairing,
   and warns that the house resets hourly. The landing page calls it
   "coming soon".
2. Abuse contact to publish (`--abuse-contact`): a mail address or a URL?
3. Rate limiting: accept none, or a custom Caddy build (`caddy-ratelimit`)
   or CloudFront + WAF (cost)?
4. SSH: `lightsail-connect` only, or also your IP via `--ssh-cidr`?
5. Budget alert address; is account-wide the right scope?
