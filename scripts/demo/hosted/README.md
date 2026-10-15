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
| `bootstrap.sh` | host, once | Swap, SSH hardening, fail2ban, unattended upgrades, Docker (official repo, key pinned), Node (checksum pinned), 6 Mbit/s egress cap, internal network, Caddy, timers, first build, listener + egress checks. |
| `reset.sh` | host, hourly | `down.sh`, restore the clone to the pinned sha, `up.sh` with this overlay, relay with a fresh token, dashboard, `connect.json`. |
| `guard.sh` | host, every minute | Disk/swap watchdog; `demo.mjs guard`; relay health. Anything wrong → full reset. |
| `demo.mjs` | host | `setup` (http trial promote, local-only owner, admin `demo/demo`, drop onboarding extras), `token` (demo user's LLAT for the relay), `dashboard`, `guard`. |
| `edge.yml`, `Caddyfile` | host | Caddy in its own compose project: TLS, CSP, blocked paths, resetting page, `/relay/*`, `/connect.json`, `/abuse`, privacy-filtered access log. |
| `docker-compose.yml` | host | Overlay over the test bed compose: internal network only, sealed directories, limits, relay settings, digest pins. |
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
  or frames from another origin in anyone's browser (CSP); delete or
  deactivate the owner (HA refuses); keep a changed `demo` password (reset
  within a minute); keep anything past :00.
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
- During the rebuild Caddy serves "Demo is resetting" (503, auto-refresh 15 s).
  For ~20 s after HA answers, the house is still being seeded.
- In HA: `sensor.demo_next_reset` (timestamp; agents can read it:
  `ha-nova get sensor.demo_next_reset`), a banner as the header of every
  view of the default dashboard (minutes to the reset, "don't enter personal
  data", `/abuse`), and a persistent notification at :55.
- Visitors can edit or remove the banner and the :55 automation; both come
  back at :00.

## Connecting an AI client

`https://demo.hanova.app/connect.json` (regenerated every reset, never in the
repo): `ha_url`, `relay_url` (`https://demo.hanova.app/relay`),
`relay_token`, the login, `next_reset`. The relay runs with the **demo
user's** long-lived token (the owner's never leaves the host), `/files` is
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
| Egress | HA, broker, relay sit only on `ha-nova-demo-house` (`internal: true`, no route out; verified: DNS and direct IPs fail from HA and the relay). Only Caddy (ACME) and dockerd (pulls) reach the internet. Egress capped at 6 Mbit/s (`tc tbf`), ~2 TB/month. |
| SSH / OS | Key-only, no root, no passwords, no forwarding of agents/X11; fail2ban. Ubuntu 24.04, unattended upgrades (Ubuntu security + Docker repo), reboot 04:30 UTC when needed. Docker from download.docker.com (one key, fingerprint pinned), `live-restore`, daemon-wide `no-new-privileges`, unix socket only. |
| Containers | All: `no-new-privileges`, `cap_drop: [ALL]`, read-only root, CPU/memory/pids limits (HA 1.5 CPU, 640 MiB + swap to 1 GiB, 512 pids), size-capped logs, no docker.sock, no host network, nothing privileged, images pinned by digest. HA keeps 5 caps for s6-overlay; Caddy uid 1000 + `NET_BIND_SERVICE`; broker uid 1883; relay uid 100. |
| HA config surface | `/config` stays writable (`.storage`, recorder, UI editors' atomic writes), but `configuration.yaml` and `packages/` are read-only binds and `custom_components`, `python_scripts`, `www`, `backups`, `image`, `tts`, `/media` are empty read-only tmpfs mounts. `home-assistant.log` is `/dev/null`; `file_upload` lands in a 64 MiB `/tmp` tmpfs. No `default_config`; onboarding's met/google_translate/radio_browser/shopping_list entries are deleted. No shell_command/command_line/rest/python_script anywhere. go2rtc (can run `exec:` sources) has no config flow, so it cannot be added. |
| MQTT | `mqtt.publish` is an admin service (read in the 2026.10.1 source), so visitors have it. The broker gives HA a listener whose ACL allows reads and only `testbed/+/set`, `testbed/plug/factory_reset`, `testbed/router/restart` writes (verified: discovery deletes/additions dropped, commands pass). Seeding uses a second listener on the broker container's loopback. |
| Browser | CSP: scripts, images, frames, connections only from this origin (+ `data:`/`blob:`, inline for HA's own boot). Verified: an external dashboard resource and external markdown images are blocked; the HA UI works (dashboards, settings, automations, history, energy, media). HA's markdown sanitizer strips `onerror`, `javascript:` links, `<script>` (verified). HSTS, `nosniff`, `X-Frame-Options DENY`, `frame-ancestors 'none'`, 1 MB request bodies, Caddy admin API off. |
| Accounts | Guard every minute via the owner's token: owner active + local-only, `demo` present, active, admin, not local-only, http config unchanged, else full reset; re-sets the `demo` password; dismisses "Login attempt failed" notices (they show visitor IPs to everyone). HA refuses to delete or deactivate the owner (verified). |
| Proxy trust | `trusted_proxies` = Caddy's fixed 10.89.240.2 only (dynamic addresses start at .128). HA 2026.10 imports the `http:` YAML as a 5-minute trial; `demo.mjs setup` promotes it right after onboarding and fails loud otherwise. **No IP bans:** HA bans on any 401, the password is public, the owner cannot log in from outside anyway; bans would only lock visitors (or the tooling path) out. |
| Secrets | None in the repo or user-data (readable via instance metadata). Owner password and token, demo LLAT, relay token are generated on the host per reset. IMDSv2 only, hop limit 1. |
| AWS | No IAM role or credentials on the instance (Lightsail has none). Budget alarm at 10 USD. |
| Logs / privacy | Caddy access log: 7 days, client IPs truncated to /24 (/48), no headers, no `authSig`/`token`/`code` query values. `/abuse` shows the contact given at provisioning. |
| Integrity | Hourly rebuild restores the clone (`checkout --force`, `clean -ffdx`) and all HA state. Host compromise is not fixed by a reset: rebuild the instance. |

**Detect:** budget mail; `journalctl -u ha-nova-demo-guard -u ha-nova-demo-reset`
(frequent guard resets = someone breaking things); `docker stats`, `free -m`,
`df -h`; `ss -ltn`; Caddy log in `/var/lib/ha-nova-demo/logs`. **Respond:**
rebuild (`teardown.sh --keep-ip --yes && provision.sh … --yes`); rotate the
Lightsail key pair if SSH was involved.

### Verified empirically vs. read in code

Empirically, on a throwaway copy (own compose projects, names, ports; HA
2026.10.1; Docker Desktop): full reset twice (34-48 s; Caddy kept serving,
503 page during the gap), 156 entities / 6 MQTT; egress blocked from HA,
relay, broker; read-only mounts; `/dev/null` log; backup create/restore
fail; `camera.snapshot` refused/RO; MQTT ACL; owner delete/deactivate refused;
guard catches an owner change and repairs a changed `demo` password; relay
calls as demo admin, `/files` off, 8 MB store; blocked paths; CSP in a real
browser; no 401s from admin UI browsing; masked access log; CPU and pids
limits applied. Earlier round (same image): owner login from a public client
IP gets 403.

Read in code only: ban middleware (any 401 counts; login notices always),
allowlist defaults (`www` + media dirs always added), `mqtt.publish` admin-only,
go2rtc/local_file/file manifests and checks. Not run at all: `bootstrap.sh`,
systemd units, `tc`, `guard.sh`/`reset.sh` as such (their steps were run by
an equivalent local driver), every `aws` call.

### Residual risks (accepted for a simulated, hourly-reset house)

- Shared admin: anyone can break the house for everyone until :00, or
  trigger guard resets repeatedly (a 1-3 min outage each).
- Content written by one visitor (notifications with links, entity names,
  dashboards, todo items, LLATs they minted) is visible to others until :00.
  External links are allowed (CSP does not govern navigation).
- `local_file` (UI config flow, no path check) can show any file the HA
  container can read as a camera image, e.g. `.storage/auth`: other
  visitors' sessions (all the same `demo` user) and the owner's token, i.e.
  nothing beyond admin. Nothing from the host or the relay is mounted.
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

## Bumping pins

HA/Mosquitto tags live in the test bed compose; the overlay and `edge.yml`
pin digests (`docker buildx imagetools inspect <image>:<tag>`); `down.sh`
pins alpine. Node: `NODE_VERSION`/`NODE_SHA256` in `bootstrap.sh`. Then
rebuild with the new sha.

## Open items and questions

1. CLI (spec only, not implemented): `ha-nova setup <client> --demo` that
   fetches `connect.json`, accepts a relay URL with a path, skips pairing,
   and warns that the token changes hourly.
2. Abuse contact to publish (`--abuse-contact`): a mail address or a URL?
3. Rate limiting: accept none, or a custom Caddy build (`caddy-ratelimit`)
   or CloudFront + WAF (cost)?
4. SSH: `lightsail-connect` only, or also your IP via `--ssh-cidr`?
5. Budget alert address; is account-wide the right scope?
