# HA NOVA hosted demo (demo.hanova.app)

"Try before you connect": the test bed house (`scripts/e2e/testbed/`) on one
public AWS Lightsail instance, reset to a pristine seed every night. Prepared,
not deployed: nothing here has run against AWS yet.
(The parent `scripts/demo/` is the unrelated video-recording kit,
`docs/reference/demo-recording.md`.)

| File | Runs on | What it does |
| --- | --- | --- |
| `provision.sh` | your Mac | Lightsail instance, static IPv4, firewall, IMDSv2, budget alert. Prints every `aws` call; executes only with `--yes`. Idempotent. |
| `teardown.sh` | your Mac | `--keep-ip`: delete the instance (rebuild). `--dns-removed`: delete instance, static IP, budget. Prints; `--yes` executes. |
| `aws-common.sh` | your Mac | Sourced by both: every call is `--profile personal --region eu-central-1`, account `018368605240` is checked first, ambient AWS env vars are unset. |
| `bootstrap.sh` | host, once | Swap, SSH hardening, fail2ban, unattended upgrades, Docker (official repo), Node (checksum-pinned), reset timer, first boot, listener check. |
| `reset.sh` | host, nightly | Restore the clone to the pinned ref, `down.sh`, `git clean`, `up.sh` with this overlay. |
| `docker-compose.yml` | host | Overlay merged over the test bed compose: internal network, Caddy, container hardening, digest-pinned images. |
| `Caddyfile` | host | TLS (Let's Encrypt), security headers, 1 MB body limit, proxy to HA. |
| `config/` | host | Copied over the test bed config: `configuration.yaml` (no `default_config`, http/proxy/ip_ban block), `packages/commands.yaml` (masks command_line + rest). |

Test bed hooks this relies on (`up.sh`/`down.sh`): `TESTBED_OVERLAY=<this dir>`
(merge compose, copy config, HA + broker only, run `seed.mjs demo-user`) and
`TESTBED_OWNER_PASSWORD`.

## Run it

```bash
# 1. Dry run (no AWS call at all), then for real. --ref must be on GitHub and contain scripts/demo/hosted.
bash scripts/demo/hosted/provision.sh --ref <merge-commit-sha> --alert-email <you> [--ssh-cidr <your-ip>/32]
bash scripts/demo/hosted/provision.sh --ref <merge-commit-sha> --alert-email <you> --yes
# 2. Namecheap: A demo.hanova.app -> printed static IPv4, AAAA -> printed IPv6.
# 3. Lightsail browser SSH: sudo tail -f /var/log/cloud-init-output.log   (~10 min)
# 4. https://demo.hanova.app, login demo / demo
```

Rebuild from scratch (also the incident response):
`teardown.sh --keep-ip --yes && provision.sh --ref … --alert-email … --yes`
(same IPv4, A record untouched; the IPv6 changes, so update AAAA if used).
Change the ref the same way.

## Cost (eu-central-1, list prices)

| Item | USD / month |
| --- | --- |
| Lightsail `micro_3_0` (1 GB RAM, 2 vCPU burst, 40 GB SSD, 2 TB transfer, public IPv4 included) | 7.00 |
| Static IPv4 while attached | 0.00 (detached: ~0.005/h, ~3.65/month: reattach quickly) |
| AWS Budgets (one budget, no budget actions) | 0.00 |
| Transfer beyond 2 TB | ~0.09/GB (not expected) |
| **Total** | **7.00 + VAT** (19 % on a German account: ~8.33) |

Budget `ha-nova-demo-monthly`: 10 USD, account-wide (the account is private),
mail at 80 % actual and 100 % forecast. Lightsail bills hourly up to the
monthly price, so the instance alone can never exceed 7 USD.

## Access model

- **Visitors:** HA UI, user `demo` / password `demo`, group `system-users`
  (non-admin). Verified on 2026.10.1: states and ordinary service calls work;
  `/api/config/*`, `homeassistant.restart`, `/api/template`, `/api/error_log`
  return 401; WS `http/config`, `config/auth/*`, config flows, backups,
  dashboard saves, registry edits are `unauthorized`.
- **Owner** (`testbed`): random 48-hex password per reset in
  `/root/ha-nova-demo-owner-password`, `local_only: true`. A login with a
  public client IP gets 403 (verified). Maintainer access: SSH tunnel
  `-L 18123:127.0.0.1:18123`, then http://127.0.0.1:18123.
- **AI clients:** none in v1. The ha-nova CLI reaches HA only through the
  relay, and the relay is not exposed (next section). Visitors can mint their
  own non-admin long-lived token in the HA profile page; that works with
  HA-direct clients, not with ha-nova.

## Relay exposure

Standalone relay, one bearer token (`createHttpServer`). What a public token could do:

| Endpoint | Reach with the test bed's relay (owner LLAT = admin) |
| --- | --- |
| `POST /ws` | Any non-subscription HA WS command (subscriptions inside bounded `collect_events`) as the relay's HA user: create admins, delete the demo user, config flows (outbound integrations), edit/delete automations, backups, `http/config/configure` (rewrite the proxy trust that keeps the owner local-only). |
| `POST /core` | Any HA REST path under `/api/` (GET/POST/DELETE, traversal-hardened) as the same user: `/api/config/*`, admin services, `/api/template`. |
| `POST /files` | Off unless `FILE_ACCESS` is set and a config dir is mounted (the test bed mounts none: 403). With `readwrite` it writes YAML: a `shell_command`/`command_line` entry plus a restart is code execution in the HA container. Must stay off. |
| `POST /backups` | Relay-local opaque JSON store, slug paths, capped at 500 files / 50 MB. No HA access; visitors could read each other's snapshots and fill the cap. |
| `GET /health` | Version, WS status, file mode. Minor disclosure. |
| `POST /pair`, `GET /home` | Bearer-exempt. `/pair` trades a one-time six-digit code (rate-limited per peer; behind a proxy all visitors share one peer) for the relay token. |

**Recommendation: do not expose the relay in v1.** With an admin LLAT the
token is full HA admin; no endpoint restriction makes that safe.

Minimum for a later "try ha-nova" path, read-and-call only:

1. A second non-admin HA user (`demo-relay`, random password, never
   published) and the relay's `HA_LLAT` minted for it. HA itself then refuses
   every admin WS/REST command, so the token can do what `demo/demo` already
   can: read state and call non-admin services. Not covered: writing
   automations/scripts (admin-only config API), the main ha-nova write flow.
2. `FILE_ACCESS` unset (off), no config mount, `SNAPSHOT_DIR` on tmpfs.
3. Caddy route `relay.demo.hanova.app` (or `/relay/*`) to the relay only;
   relay on the internal network (no egress).
4. A published shared token and the README's `ha-nova setup … --relay-token`
   line for visitors. Accept: anyone can revoke the relay user's own refresh
   token (`auth/delete_refresh_token`) and break it until the next reset.

## Threat model + hardening

Assumed attacker: anyone on the internet, with the public demo login and this
repository. Goals to deny: code execution on the host, use of the host for
outbound abuse, access to the AWS account, persistence past a rebuild.

| Layer | Control |
| --- | --- |
| Network | Lightsail firewall replaced as a whole: 80/443 tcp + 443 udp public (IPv4 and IPv6), 22 tcp IPv4-only for `lightsail-connect` (browser SSH) plus an optional single `--ssh-cidr`; no IPv6 on 22. `provision.sh` re-reads the port states and fails if 22 is open to `0.0.0.0/0`/`::/0` or any other port is open. `bootstrap.sh` fails if anything except 22/80/443 listens beyond loopback (TCP). Only Caddy publishes ports; its 18123 seeding port binds 127.0.0.1. |
| SSH | Key-only (Lightsail key pair), no root login, no password/keyboard-interactive auth, no agent/X11 forwarding; fail2ban `sshd` jail. |
| OS | Ubuntu 24.04 with unattended upgrades (Ubuntu security + Docker repo), automatic reboot at 04:30 UTC when needed; Docker from download.docker.com with the key fingerprint pinned; `live-restore`, daemon-wide `no-new-privileges`, unix socket only (checked); 1 GB swapfile. |
| Containers | All: `no-new-privileges`, `cap_drop: [ALL]`, memory + pids limits, `restart: unless-stopped`, size-capped logs, no docker.sock, no host network, nothing privileged, images pinned by digest. Caddy: uid 1000, read-only root, only `NET_BIND_SERVICE`. Mosquitto: uid 1883, read-only root. HA: read-only root (tmpfs `/run` exec + `/tmp`), root inside (s6-overlay) with `CHOWN DAC_OVERRIDE FOWNER SETGID SETUID` only. |
| Egress | HA and the broker sit on an `internal: true` network: no route out. SSRF, outbound scans, spam, mining callbacks: blocked. Only Caddy (ACME) and dockerd (image pulls) reach the internet. |
| HA | No `default_config` (no cloud, discovery, backup, mobile_app, go2rtc, alerts); no shell_command, command_line, rest, python_script, file integrations, Supervisor. Public user non-admin; owner local-only with a per-reset random password. `http`: `use_x_forwarded_for` with `trusted_proxies` = Caddy's fixed IP only, `ip_ban_enabled`, `login_attempts_threshold: 10`. The webhook automation is `local_only`. |
| Caddy | Automatic HTTPS, HSTS, `nosniff`, `X-Frame-Options DENY`, referrer and permissions policies, `Server` header removed, request bodies capped at 1 MB, admin API off, client-sent `X-Forwarded-For` replaced. **No rate limiting:** Caddy core has none (`caddy list-modules` on 2.11.4 shows no rate-limit module); it needs the third-party `caddy-ratelimit` plugin and a custom build, so it is skipped. HA's ip_ban is the only throttle (failed logins and 401s). |
| Secrets | None in the repo or in user-data (user-data is readable from instance metadata): the owner password and HA token are generated on the host per reset, mode 600 under `/root` and the root-owned clone. Instance metadata is IMDSv2-only with hop limit 1, so containers cannot reach it even with egress. |
| AWS blast radius | Lightsail instances carry no IAM role and no credentials; nothing on the host can call AWS. Budget alarm at 10 USD. |
| Integrity | Nightly `reset.sh` restores the clone (`checkout --force` + `clean -ffdx`) and all HA state. A suspected host compromise is not fixed by a reset: rebuild (`teardown.sh --keep-ip --yes && provision.sh … --yes`). |

**Detect:** budget mail; `systemctl status ha-nova-demo-reset` (a failed reset
leaves the old house running); `docker stats` / `free -m` for runaway
memory; `journalctl -u ssh` and `fail2ban-client status sshd`;
`/var/lib/docker` growth; an unexpected listener (re-run the bootstrap check:
`ss -ltn`). **Respond:** rebuild, then rotate the Lightsail key pair if SSH
was involved.

### Residual risks (accepted for a simulated, nightly-reset house)

- Visitors share one account: `demo` can change its own password (locks
  others out until the reset), and anything a visitor writes (input_text,
  persistent notifications, todo items) is visible to the next visitor
  (renames and dashboard edits are admin-only). Mitigation if abused: run the reset hourly.
- No request rate limit: a flood or heavy `render_template` calls (allowed
  for non-admins) can slow or stall HA until it stops. Fix path: custom Caddy
  build with `caddy-ratelimit`, or CloudFront + WAF in front (cost).
- ip_ban bans per IP until the reset; a shared NAT can lock out a whole office.
- HA 2026.10 migrates the `http:` YAML once into storage as a pending trial and
  reverts it after 5 minutes unless an admin promotes it; `seed.mjs demo-user`
  promotes it right after onboarding and fails loud if the stored config lacks
  the proxy. HA raises a `yaml_still_present_after_migration` repair
  (breaks in 2027.2): before that release, set the config through
  `http/config/configure` instead of YAML.

## Memory budget (1 GB RAM)

Measured locally on 2026.10.1 with the full seed and every limit above: HA
270-310 MiB, Caddy ~18 MiB, Mosquitto ~2 MiB. Estimated on the host: Ubuntu +
agents ~150-200 MiB, dockerd/containerd ~80-100 MiB, HA growing to ~400 MiB
with a day of recorder data. ~650-750 MiB in use of ~950 MiB, so it fits
without swapping in steady state; the 1 GB swapfile absorbs boot and reset
peaks. HA is capped at 640 MiB RAM + swap up to 1 GiB, so a runaway HA is
OOM-killed and restarted instead of taking the host down. The relay is not
built or run on the host (its `npm ci` build alone would need swap).

## Verified locally (Docker Desktop, 2026-10-15)

`check_config` passes for the test bed config and the demo overlay; HA boots
read-only with the capability set above on an internal network; Caddy runs as
uid 1000 with `NET_BIND_SERVICE` only; onboarding, `demo-user` (twice:
idempotent), the full house seed (156 entities, 6 MQTT), the non-admin
probes, and the owner 403 from a public client IP all pass through Caddy.
`docker compose config` validates the merged files. Not run: `up.sh`/`reset.sh`
end to end with the overlay, `bootstrap.sh`, and every `aws` call.

## Bumping pins

HA/Mosquitto tags live in the test bed compose; this overlay pins the same
tags by digest (`docker buildx imagetools inspect <image>:<tag>`). Bump both
together, then `teardown.sh --keep-ip` + `provision.sh` with the new ref.
Node: `NODE_VERSION`/`NODE_SHA256` in `bootstrap.sh` (from nodejs.org
`SHASUMS256.txt`).

## Open questions for the maintainer

1. Relay: keep it unexposed (v1 = HA UI only), or build the non-admin
   read-and-call path above? Writes would still not work for visitors.
2. Reset cadence: nightly (03:30 UTC) as specified, or hourly given the
   shared `demo` account?
3. Is a demo without rate limiting acceptable, or do you want a custom Caddy
   build (`caddy-ratelimit`, no longer a stock image) or CloudFront + WAF?
4. SSH: `lightsail-connect` (browser SSH) only, or also your IP via
   `--ssh-cidr`?
5. AAAA record: publish IPv6 too? The instance IPv6 changes on every rebuild
   (only the IPv4 is static), so the AAAA record needs an update each time.
6. Budget alert address, and whether account-wide (any spend in the private
   account) is the right scope.
