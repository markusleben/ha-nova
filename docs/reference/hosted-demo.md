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
| `bootstrap.sh` | host, once per ref | Refuses unless the clone is at `--ref`. Swap, SSH hardening, fail2ban, unattended upgrades, Docker (official repo, key pinned), Node + cosign (checksums pinned), 6 Mbit/s egress cap, internal network, Caddy (validated, recreated), the relay and chat bridge images for this ref, the env (only after both exist), the first gated reset, then the timers. Re-runs pause the timers until done (restarted on failure too). |
| `reset.sh` | host, hourly + 2 min after boot | Under the lock: hide the house (before any check that can fail), stop HA and keep the chat-app sign-ins (`chat.mjs keep`), `down.sh`, restore the clone to the pinned sha, restart Caddy if the name points here but nothing is served over TLS yet, seed the `ha_nova` entry, `up.sh` with this overlay and the verified HA digest, the kept sign-ins back in (HA stopped for it; skipped when there are none), relay + chat bridge, gates, dashboard, `connect.json`, expose, public gate. |
| `guard.sh` | host, every minute | Skips while a reset holds the lock. Disk/swap watchdog; `demo.mjs guard` (30 s cap); `chat.mjs check` (15 s cap); relay round trip (revoked token: reset at once; other failures: after 3 minutes); TLS that came up after a skipped public gate. Anything wrong → full reset. |
| `update-ha.sh` | host, nightly | Latest stable HA: resolve digest, verify the Sigstore signature, refuse downgrades, candidate reset + gates, promote or roll back, prune old images. |
| `host-lib.sh` | host | Shared gates: relay round trip, egress/DNS, listeners, public site (with the Add to Claude metadata); Caddy (re)creation; pinned Node/Go/Caddy images; the lock. |
| `chat.mjs`, `Dockerfile.bridge` | host | Add to Claude (below): `entry` (the seeded `ha_nova` entry), `files` (the bridge's secret, demo profile, `chat-apps.json`), `gate` (sign-in as `demo` the way Claude does, tools work, the owner refused), `check` (guard: puts `external_url` back, entry loaded, owner refused), `keep`/`restore` (the chat-app sign-ins across the reset, below). The bridge image (`ha-nova serve` + skills of this ref), built once per ref by `bootstrap.sh`. |
| `demo.mjs` | host | `setup` (seeded http config check, local-only owner, admin `demo/demo`, drop onboarding extras), `token` (the relay user's LLAT), `dashboard` (first saves HA NOVA's sample work, then the dashboard with "Start here" first; also shows the "Demo add-on" device as "Demo app", drops the Map dashboard, arms the simulated MQTT devices), `guard`, `smoke` (incl. an MQTT device round trip). |
| `edge.yml`, `Caddyfile`, `site/` | host | Caddy in its own compose project: TLS, CSP, blocked paths, landing redirect, restarting page (`site/icon.svg`: a copy of `assets/icon.svg`, kept equal by `tests/config/hosted-demo-icon.test.ts`), `/relay/*`, `/connect.json`, `/abuse`, privacy-filtered access log. |
| `docker-compose.yml` | host | Overlay over the test bed compose: internal network only, DNS sink, sealed directories, the `ha_nova` integration (read-only), limits, relay and chat bridge settings. |
| `mosquitto.conf`, `mosquitto.acl` | host | Broker split: seeding listener (container loopback) vs. Home Assistant listener (no discovery writes). |
| `config/` | host | Over the test bed config: `configuration.yaml`, `packages/demo.yaml` (reset sensor + :55 warning; "Demo: simulated devices report back" and "Demo: relock the entrance door", In HA below), `packages/commands.yaml` (masks command_line + rest). |

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
# 4. https://demo.hanova.app -> hanova.app/demo/ -> Add to Claude, or "Open the house next to the chat", login demo / demo
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
- **Cannot** (verified on 2026.10.1): reach the internet from HA, the relay, or the chat bridge;
  create, upload, or restore backups (empty read-only `/config/backups`;
  Caddy 403s `/api/backup/*`); change what HA loads (`configuration.yaml`,
  `packages/` read-only; no `custom_components` but the read-only `ha_nova`,
  no `python_scripts`); write any
  file a service can target (`camera.snapshot`/`notify.file` paths must be in
  `www` or `/media`, both empty read-only); host files publicly (`/local/*`,
  `/api/tts_proxy/*` 403); add or delete MQTT discovery entities
  (`mqtt.publish` reaches only device command and state topics); load scripts, images,
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
  locally, estimated 1-3 min on the 1 GB host, plus one Home Assistant
  restart when chat-app sign-ins are restored (4 s on the local test bed;
  capped at 3 min). The guard also triggers it on
  demand. `reset.sh`, `guard.sh`, and `update-ha.sh` share one lock.
- The house is exposed only after every gate passed: `reset.sh` removes
  `/var/lib/ha-nova-demo/public/ready` first and touches it last. Until then,
  and whenever HA is gone or answers 502/503/504, Caddy serves "Demo is
  restarting" (503, auto-refresh 15 s, links to `/start` and Privacy; styled like
  hanova.app, its star `/_demo/icon.svg` is served while down); `/relay/*` answers
  `{"error":"demo_resetting"}` (503) when the relay is gone or the house not
  ready, while the relay's own JSON errors (it uses 502 for upstream
  failures) pass through.
- Gates in every reset (a failure takes the house down again and logs
  `journalctl -t ha-nova-demo-reset -p err`): relay round trip (`/core` GET
  `/api/`, `/ws` get_states, `/health` connected), Add to Claude
  (`chat.mjs gate`: sign-in as `demo` with Claude's identity and PKCE,
  `initialize`, `tools/list`, `ha_get`, the owner refused), egress + DNS (4 s
  probes in parallel: external name and IP from HA, the relay, and the chat
  bridge must fail, while
  the same tools must reach house targets and service names must resolve),
  listeners, then the public site through Caddy's TLS on the host (`/`
  redirects to the landing page, `/?auth_callback=1` reaches HA,
  `/connect.json` with its CORS header, `/_demo/icon.svg`, `/relay/health`
  with the public token, the login page, `/api/ha_nova/mcp`'s 401 naming
  this origin's resource metadata and OAuth metadata with CIMD, `none`, S256,
  403 on one path per blocked pattern, read from the Caddyfile); skipped
  only while nothing at all is served over TLS (anything served, even with
  an expired or untrusted certificate, must pass). This also re-checks the
  sandbox after every dockerd upgrade.
- Landing page: `https://hanova.app/demo/` (`demo/index.html` in
  `markusleben/ha-nova-site`, the site's design). `/` without a query and
  `/start` answer 302 there; HA's own redirects to `/` always carry a query
  (sign-in returns to `/?auth_callback=1&code=...`) and still reach HA. It
  leads with "Try HA NOVA on a demo house": the personal-data warning above
  every button, then "Which app do you use?" (Claude: Add to Claude; ChatGPT Plus
  or Pro on the web: paste the address, OAuth, Connect again if the first
  sign-in fails; a computer: install, then `ha-nova try`), three
  prompts, a small "Open the house next to the chat and watch it change"
  link (`/demo-house/house`), the login, admin rights, the next reset, the
  running HA version (read from `/connect.json`, which answers GETs with
  `Access-Control-Allow-Origin: https://hanova.app`), and the abuse link.
  It promises nothing unreleased. Deploy this
  Caddyfile only once that page is live (`bootstrap.sh` refuses otherwise). Verified locally (throwaway HA
  2026.10.1 behind this Caddyfile): logged-out `/` and `/start`, sign-in
  back to a deep link and to `/?auth_callback=1`, logout (HA goes to `/`)
  lands on the landing page.
- In HA: `sensor.demo_next_reset` (UTC timestamp; agents:
  `ha-nova get sensor.demo_next_reset`), the `demo-house` dashboard as the
  system-wide default with a banner as every view's header (the personal-data
  warning first, minutes to the reset, HA version, links to `/start`,
  `/abuse`, and Privacy), and a notice at :55 UTC (with the same warning).
  Both use `utcnow()`, because visitors can change HA's time zone.
- The dashboard opens on "Start here" (`/demo-house/start`): what HA NOVA is,
  how to try it (Claude through hanova.app/demo, or `ha-nova try`), five prompts (the first
  four are `ha-nova try`'s, kept equal by
  `tests/config/hosted-demo-start-here.test.ts`), what HA NOVA made here, and
  what "What is worth a look?" finds (the demo integration's 12 % battery,
  devices with no room). "Made with HA NOVA": two automations and two scripts
  in `made-with-nova.json`, made by the write skill (Claude Code with this
  repo's skills, local test bed, 2026-10-21) and saved as Home Assistant read
  them back; only each description was replaced by "Made with HA NOVA from:
  <what was asked>". `demo.mjs dashboard` posts them to the config API (the
  one `ha-nova apply` writes to) every reset. Re-generate them the same way
  when the house changes. "Watch the house" links (`ha-nova try`, the setup
  page, hanova.app/demo) go to `/demo-house/house`.
- Every tile on the dashboard reacts. The test bed's MQTT devices (entrance
  lock, garage, driveway gate, smart plug) have no device behind them, so
  "Demo: simulated devices report back" publishes each command as the new
  state (demo only; the test bed keeps them silent). `ha-nova try` has
  visitors unlock the entrance door: "Demo: relock the entrance door" locks
  it again 10 minutes later, so the next visitor gets the same demo. These
  two and the reset notice stay off the dashboard, and the guard turns them
  back on within a minute if someone turns them off in Settings (not if
  someone disables the entity in the registry: that lasts until :00).
  `demo.mjs dashboard` waits for the lock to answer before the house goes
  public; if it does not, the reset logs a warning and goes on (the house
  works without it; `smoke` gates new HA releases on it). Also off the
  dashboard: the demo integration's deliberately inert entities (`lock.poorly_installed_door`
  jams, `vacuum.demo_vacuum_3_third_floor`/`_4_fourth_floor` have no
  controls, `media_player.browse`/`search` only browse). Also inert on
  purpose, off the dashboard anyway: `alarm_control_panel.security` (code
  `1234`), `update.demo_update_no_install`.
- No Map dashboard: map tiles come from OpenStreetMap through HA, which has
  no egress, so it would stay empty. Settings > Zones still shows its map
  without tiles (the zone circles draw; the console logs a 503 for
  `/api/map_tiles/tilejson.json`).
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

## Add to Claude

Visitors with a claude.ai account (the free plan allows one custom
connector) add `https://demo.hanova.app/api/ha_nova/mcp` (the landing page's "Add to
Claude" button opens Claude's "Add custom connector" with it filled in) and
sign in as `demo` / `demo`. Design and reasons:
`docs/work/2026-10-20-demo-add-to-claude-spec.md`. ChatGPT (Plus or Pro, on
the web) works with the same address pasted as a custom MCP server with
OAuth (maintainer test, ChatGPT Pro, 2026-10-22); its first sign-in often
fails with "OAuth callback data missing", and Connect again works. The free
plan can't add its own connectors.

- Path: claude.ai's servers → Caddy → Home Assistant's own OAuth and the
  `ha_nova` integration (`nova/integration`, the only `custom_components`,
  read-only; its `hassio`-source entry with a fresh secret is seeded into
  `.storage` before the first boot) → the bridge container
  (`ha-nova serve --server demo --rate-limit 120`, uid 1000, no egress) →
  the relay with the public token → Home Assistant.
- Only the `demo` user of the current build may use it (`chat-apps.json`).
  The owner (local-only, not listed), `nova-relay`, and users visitors create
  get the bridge's 403.
- `external_url` comes from `DEMO_EXTERNAL_URL` in `configuration.yaml`, so
  the OAuth metadata names `https://<domain>` and visitors can't change it.
  Claude's published identity and callback share the claude.ai host: Home
  Assistant accepts them without fetching anything.
- Chat-app sign-ins survive the reset; everything else in the auth store
  goes. Before `down.sh`, `reset.sh` stops HA (it saves its auth store on
  stop) and `chat.mjs keep` copies refresh tokens that are all of: the `demo`
  user's (by the id recorded at creation), type `normal`, one of the
  published client ids (CIMD; HA offers no DCR and stores no client
  registration): Claude's `https://claude.ai/oauth/mcp-oauth-client-metadata`
  or ChatGPT's `https://chatgpt.com/oauth/client.json` /
  `https://chatgpt.com/oauth/<callback id>/client.json` (at most 64 URL-safe
  characters, no dots), and used in the last 24 h; the newest 200 (`/var/lib/ha-nova-demo/chat-sign-ins.json`, root
  0600, not served). After `up.sh`, `chat.mjs restore` adds them to the new
  store while HA is stopped, bound to the new `demo` user and its
  credential. Only id, token, signing key, client id, and the two times are
  copied, each checked; the rest is set fresh (no IP, no name or icon, 30-min
  access tokens, HA's 90-day expiry). Files are read without following
  links. A store that is missing (a reset that failed before `up.sh`) keeps
  the last carry file; one that is present but unreadable, over 32 MiB, or
  not a plain file drops it. A gate that fails after a restore moves the
  carry file to `chat-sign-ins.json.failed`, so a bad restore can't keep the
  house down. Browser sign-ins, long-lived tokens, and other users' tokens are
  not kept: a browser signs in again with `demo` / `demo` in one step.
  Visitors' changes still go at :00, and a kept token reaches only the demo
  house as `demo`, as the public login does.
- A sign-in that is not kept (unused for 24 h, past the newest 200, or
  removed by another visitor in Profile → Security) gets HA's
  `invalid_grant` on its next refresh, and the chat app has to connect again.
  If ChatGPT then keeps spinning on "Connect", remove the plugin in ChatGPT
  and add it again.
- Incident (a leaked or abused chat-app sign-in): on the host, run
  `sudo DEMO_DROP_SIGN_INS=1 /opt/ha-nova/scripts/demo/hosted/reset.sh` once;
  every chat-app sign-in is dropped and every chat app connects again. To
  keep dropping them at every reset, add `DEMO_DROP_SIGN_INS=1` to
  `/etc/ha-nova-demo.env` (remove the line to go back).
- While the house is down, or HA answers 502-504, `/auth/token` gets a JSON OAuth error
  (`temporarily_unavailable`, 503, `Retry-After`), never the HTML page, so a
  chat app's refresh during the reset can be retried.
- Within 10 minutes of :00, every typed result of the demo house (chat bridge and
  `ha-nova try`) carries a `demo_reset` notice for the user: "The demo house
  resets in 6 minutes. Your connection stays, your changes don't." (results
  only: tool descriptions are unchanged).
  Risky changes show the chat code (no window, no phone here).
- Caddy answers `/api/ha_nova/mcp` with a JSON-RPC error ("The demo house
  is resetting …", 503) while the house is down, never the HTML page.

## Threat model + hardening

Assumed attacker: anyone on the internet with admin rights in this HA and
this repository. Denied goals: code execution outside HA's own sandbox, the
host or demo as an attack platform (outbound traffic, hosting files or
scripts for third parties), the AWS account, persistence past :00 (the
kept chat-app sign-ins reach only the demo house as `demo`, Add to Claude).

| Layer | Control |
| --- | --- |
| Network | IPv4-only instance (Docker's userland proxy would hand every IPv6 visitor to HA as one bridge address). Lightsail firewall replaced as a whole right after the create call (retried until the API accepts it for the pending instance; every re-run converges it again, so an aborted run leaves nothing open for long): 80/443 tcp + 443 udp public, 22 tcp only for `lightsail-connect` (+ one `--ssh-cidr`); `provision.sh` then re-reads the port states and IMDS options and fails on any other open port, a public 22, or optional IMDS tokens. Every reset fails on any listener beyond loopback other than 22/80/443. Only Caddy publishes ports; its tooling port binds 127.0.0.1. |
| Egress | HA, broker, relay, chat bridge sit only on `ha-nova-demo-house` (`internal: true`, no route out). Linux dockerd's embedded resolver can still forward external names from internal networks (a DNS exfiltration channel), so all four use `dns: 192.0.2.1` (TEST-NET-1, unroutable): service names resolve, external ones time out. Every reset (bootstrap's first included) fails if an external name or address is reachable from HA, the relay, or the chat bridge, or if the probes cannot reach house targets (a missing tool must not pass). Only Caddy (ACME) and dockerd (pulls) reach the internet. Egress capped at 6 Mbit/s (`tc tbf`), ~2 TB/month. |
| SSH / OS | Key-only, no root, no passwords, no forwarding of agents/X11; fail2ban. Ubuntu 24.04, unattended upgrades (Ubuntu security + Docker repo), reboot 04:30 UTC when needed. Docker from download.docker.com (one key, fingerprint pinned), `live-restore`, daemon-wide `no-new-privileges`, unix socket only. |
| Containers | All: `no-new-privileges`, `cap_drop: [ALL]`, read-only root, CPU/memory/pids limits (HA 1.5 CPU, 640 MiB + swap to 1 GiB, 512 pids), size-capped logs, no docker.sock, no host network, nothing privileged, images by digest (HA: verified nightly, below). HA keeps 5 caps for s6-overlay; Caddy uid 1000 + `NET_BIND_SERVICE`; broker uid 1883; relay uid 100; chat bridge uid 1000 (0.5 CPU, 128 MiB). |
| HA config surface | `/config` stays writable (`.storage`, recorder, UI editors' atomic writes), but `configuration.yaml` and `packages/` are read-only binds, `custom_components` is a read-only bind of `nova/integration` (`ha_nova` only), and `python_scripts`, `www`, `backups`, `image`, `tts`, `/media` are empty read-only tmpfs mounts. `home-assistant.log` is `/dev/null`; `file_upload` lands in a 64 MiB `/tmp` tmpfs. No `default_config`; onboarding's met/google_translate/radio_browser/shopping_list entries are deleted. No shell_command/command_line/rest/python_script anywhere. go2rtc (can run `exec:` sources) has no config flow, so it cannot be added. |
| MQTT | `mqtt.publish` is an admin service (read in the 2026.10.1 source), so visitors have it. The broker gives HA a listener whose ACL allows reads and only `testbed/+/set`, `testbed/+/state` (the simulated devices' answers), `testbed/plug/factory_reset`, `testbed/router/restart` writes (verified: discovery deletes/additions dropped, commands pass). A visitor publishing a fake state only moves a fake device. Seeding uses a second listener on the broker container's loopback. |
| Browser | CSP: scripts and frames only from this origin (+ `data:`/`blob:`, inline for HA's own boot); images and connections from this origin and `https://brands.home-assistant.io` only. HA 2026.10 serves integration icons at `/api/brands/integration/<domain>/<file>` by fetching that CDN server-side, which the house cannot; Caddy answers those paths (a `[a-z0-9_]` domain and HA's eight icon/logo file names only, no query kept) with a 302 to the same public image, so the visitor's browser fetches it and the house still has no egress. The CDN is in `connect-src` too because HA's service worker loads `/api/brands/` images with `fetch()` and follows the 302 under its own CSP (this header, on `/sw-modern.js` too); without it every icon broke once the worker was active (verified in headless Chrome on the local topology: 4/4 icons broken before, 4/4 load and land in the worker's `brands` cache after, the redirect drops `token`, the CDN request carries no `Referer`). Map tiles (`/api/map_tiles/`) are not redirected: that would add OpenStreetMap's servers to `connect-src` and to the privacy page, and skip the caching HA's proxy does for OSM's tile policy; the Map dashboard is dropped instead (In HA, above). Known, harmless: on install, HA's service worker caches `/` as its offline page; for Caddy that is the landing redirect, so `connect-src` blocks it (one console line per worker install, nothing cached; serving HA there instead would send returning visitors past the landing page). Verified: an external dashboard resource and external markdown images are blocked; the HA UI works (dashboards, settings, automations, history, energy, media). HA's markdown sanitizer strips `onerror`, `javascript:` links, `<script>` (verified). HSTS, `nosniff`, `X-Frame-Options DENY`, `Referrer-Policy: no-referrer`, `frame-ancestors 'none'`, 1 MB request bodies, Caddy admin API off. |
| Accounts | Guard every minute via the owner's token (WS handshake deadline 15 s, whole check 30 s): owner active + local-only, `demo` and `nova-relay` checked by the user ids recorded at creation (same username, active, admin, not local-only; a deleted and re-created look-alike fails), http config (`use_x_forwarded_for`, no IP bans, trusted proxy, nothing pending, running the stable slot) unchanged, relay not reporting an `auth` disconnect, else full reset; the `ha_nova` entry loaded and the owner refused by the chat bridge (`chat.mjs check`: a missing or disabled entry or an owner let in resets at once, no answer after 3 minutes). Repairs: the `demo` password; `external_url` back to `https://<domain>` and no `internal_url` (Settings > Network can change them); demo.yaml's three automations turned back on; failed-login notices (truncated visitor IPs) and notices that look like links (`//`, markdown links and references, `href`, `www.`) dismissed; banner dashboard and default panel restored. HA refuses to delete or deactivate the owner (verified). |
| Proxy trust | `trusted_proxies` = Caddy's fixed 10.89.240.2 only (dynamic addresses start at .128). Caddy replaces any client-sent `X-Forwarded-For` and sends HA the client's IPv4 with the last byte zeroed (`map` + `header_up`; a private address stays private, so the owner's local-only check is unchanged; anything not IPv4 becomes `192.0.2.0`). Verified on HA 2026.10.1 behind this Caddyfile: a failed login logs and notifies `x.y.z.0`, a sign-in's refresh token stores `last_used_ip` `x.y.z.0`, a spoofed `X-Forwarded-For` is ignored. The relay gets the same truncated header and reads only its socket peer (Caddy) anyway. HA 2026.10 deprecated the `http:` YAML (a repair, plus a 5-minute trial to promote), so there is none: `config/.storage/http` seeds HA's own store (stable slot, YAML migration marked done) before the first boot, every reset; `demo.mjs setup` fails loud if HA runs anything else. **No IP bans:** HA bans on any 401, the password is public, the owner cannot log in from outside anyway; bans would only lock visitors (or the tooling path) out. |
| Secrets | None in the repo or user-data (readable via instance metadata). Owner password and token, relay user password and token: generated on the host per reset; the public relay token once. IMDSv2 only, hop limit 1. |
| AWS | No role or credentials of the demo's account on the instance. Lightsail's own platform role (`AmazonLightsailInstanceRole`, instance profile in an AWS-owned account) is on the IMDS for host root only: IMDSv2, hop limit 1, and the house network has no route to it (verified from every container). Budget alarm at 10 USD. |
| Logs / privacy | Caddy access log (`/var/lib/ha-nova-demo/logs`): client IPs truncated to /24 (/48), no headers, no `authSig`/`token`/`code` query values. A new file every day at 00:00 UTC by the clock (timberjack's scheduled roll, also without requests once Caddy has logged one since its start; the public gate does so every reset) and at 10 MiB; a rolled file is deleted at the first roll more than 4 days after its own (`roll_keep_for 96h`; besides the current file at most 7 rolled ones): no entry is older than 6 days. Backstop for a missed roll (Caddy down or restarted across midnight): every hourly reset deletes any log file whose first (oldest) entry is over 5 days old (its mtime if that entry is unreadable; the open `access.log` is emptied instead); that day of slack covers late or skipped resets, so while the host runs no entry outlives the privacy page's 6 days. Caddy's own log (`docker logs`, size-capped, no time limit) drops the `request` object from every entry and replaces IPv4 addresses in error texts with `[ip]`, so it holds no visitor address, header, or URI (verified with an aborted proxied download). Home Assistant only ever gets the visitor's IPv4 with the last byte zeroed (Proxy trust): refresh tokens' last-used IP, failed-login notices, and HA's log all show that; everything HA stores is wiped at :00, except the chat-app sign-ins above (no address in them). `/abuse` shows the contact given at provisioning. The banner, the restarting page, and `/abuse` link hanova.app's privacy page (`https://hanova.app/privacy.html`). Visitors' browsers fetch integration icons from `brands.home-assistant.io` (their IP; no referrer: `Referrer-Policy: no-referrer` on every page, the restarting page included); `/` and `/start` redirect to the landing page on hanova.app (GitHub Pages), which in turn fetches `/connect.json`; no other third party. Add to Claude (and ChatGPT: OpenAI's servers, the same way): Anthropic's servers call `/.well-known/oauth-*`, `/auth/token`, and `/api/ha_nova/mcp` for visitors who added the demo (the access log has Anthropic's addresses, truncated, for those; the sign-in page itself opens in the visitor's browser); HA keeps the resulting refresh token (client `claude.ai` or `chatgpt.com`) until the first reset more than 24 h after its last use (so up to about 25 h), carried across resets without its last-used IP (above); the bridge logs method, tool, status, duration, and a hash prefix of the user (no content, no address) in its container log, and keeps plans (the changes Claude prepares) in `.state/bridge` until they expire or the reset; both go at every reset. |
| Integrity | Hourly rebuild restores the clone (`checkout --force`, `clean -ffdx`) and all HA state except the kept chat-app sign-ins (Add to Claude). Caddy is validated with the pinned image and recreated whenever `edge.yml`, the Caddyfile, or `site/` change (hash compare in every reset; `bootstrap.sh` always). Host compromise is not fixed by a reset: rebuild the instance. |

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
- No rate limiting at the edge or in HA (Caddy core has none; `caddy
  list-modules` on 2.11.4). A flood or heavy templates/automations can stall
  HA; bounded by HA's CPU and memory limits (the host and guard stay
  responsive) and the hourly reset. Only the chat bridge limits (120 requests
  a minute for `demo`, which every Claude visitor shares; tool calls run one
  at a time, so one heavy user can block the others for a few minutes;
  Claude's requests all come from Anthropic's addresses (ChatGPT's from OpenAI's), so per-IP limits
  would not help). The bridge keeps at most 20 open plans for `demo`: a
  visitor making many can push out another visitor's pending code.
- Add to Claude: other visitors' names, notices, and entity texts reach a
  visitor's Claude as tool results (prompt injection), both ways: planted
  text can try to make another visitor's Claude copy that visitor's data
  from other connectors into the house. The demo notice in every result says
  to treat house texts as data and never to copy the user's data in; the
  landing page says to use a new chat with no files or other connectors. Any visitor can delete
  the others' Claude sign-ins (Profile → Security); they connect again.
- Kept chat-app sign-ins: anyone can mint them (the sign-in API with
  `demo` / `demo` and Claude's client id), so many fresh ones can push real
  visitors' sign-ins past the newest 200; those visitors connect again, as
  before this was kept. A token read through `local_file` stays usable past
  :00 for as long as it is kept, but only as `demo`, which the public login
  gives anyway. Owner and other users' tokens are never kept, so forged owner
  access still ends at the reset.
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
chat bridge ~10 MiB (2026-10-20, local), broker ~3 MiB. The bridge image's
Go build (bootstrap, once per ref, `-p=1`) peaks at about 700 MiB including
swap (finished under a 400 MiB memory cap with swap, locally). Estimated host total with Ubuntu (~150-200 MiB) and Docker
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
`host-lib.sh`), alpine (`down.sh`), Node for the relay and bridge images and
Go for the bridge build (`host-lib.sh`; Go follows `cli/go.mod`),
Node and cosign on the host (`bootstrap.sh`, version + SHA256). Then rebuild
with the new sha. When a release raises `min_relay_version`, the demo's
relay (built from the provisioned ref) is too old for that CLI: re-provision
with the release's merge commit (`docs/releasing.md`).

## Open items and questions

1. CLI: done as `ha-nova try` in 0.27 (`docs/work/2026-10-18-onboarding-0.27-spec.md`
   §8); the public page names it only once that release ships.
2. Abuse contact to publish (`--abuse-contact`): a mail address or a URL?
3. Rate limiting: the chat bridge has one (Add to Claude); for HA and the
   relay, accept none, or a custom Caddy build (`caddy-ratelimit`) or
   CloudFront + WAF (cost)?
4. SSH: `lightsail-connect` only, or also your IP via `--ssh-cidr`?
5. Budget alert address; is account-wide the right scope?
