# NOVA Relay

The NOVA Relay connects your AI app to Home Assistant. That can be an AI app on
your computer. With chat apps on, it can also be Claude (also in its phone app)
or ChatGPT on the web, on a computer. Chat apps need Home Assistant Link with
remote access (a subscription from the Home Assistant team) or your own HTTPS
address. The Relay lets in only the computers and apps you allowed. It also adds
a few things Home Assistant can't do on its own.

## How It Works

```
At home:     AI app → NOVA Relay (this App) → Home Assistant
Away (Link): AI app → Home Assistant Link → Home Assistant → NOVA Relay → Home Assistant
Chat apps:   Claude / ChatGPT → Home Assistant (Link or your own HTTPS address)
             → ha_nova integration → chat-app bridge (this App) → NOVA Relay → Home Assistant
```

Your AI apps (Claude Code, Claude Desktop, Codex, OpenCode, Google Antigravity,
Hermes Agent) talk to the Relay with this computer's key. Each computer gets
its own key when it connects, and every AI app on it shares that key, so you
never copy or paste one. The know-how lives in the AI
app's skills, not in the Relay.

Home Assistant Link (formerly Home Assistant Cloud; Nabu Casa) is optional and
still in Beta. It uses the remote access you already pay for. HA NOVA adds no
server of its own in between. Without Link, everything works at home as before.
Your Home Assistant may still show the old name in Settings until the new name
reaches it. The `ha-nova cloud` commands and the `cloud` route keep their names.

## The NOVA page

Open **NOVA** in the Home Assistant sidebar to manage everything in one place.
The **Open Web UI** button on this App's page opens the same page.

There you can:

- see whether the Relay is connected to Home Assistant,
- check for App updates,
- **connect a device**: click *Connect a device* to show a six-digit code.
  Type it on your computer when `ha-nova setup` asks. The code works once, for
  10 minutes. When setup connects through your Home Assistant sign-in instead,
  the page shows that a computer is connecting, with Cancel,
- see connected devices and **revoke** any one of them at once,
- turn **chat apps** (HA NOVA in Claude and ChatGPT) on or off, let them only
  read, or turn off codes in the chat (see [Chat apps](#chat-apps-claude-and-chatgpt)),
- pick **who can use** chat apps and at what level, link each person's phone,
  and see **recent changes from chat apps**.

Only the Home Assistant owner can open this page. It opens only from inside
Home Assistant, never by its network address.

## Configuration

| Option | Description |
|--------|-------------|
| **Relay Auth Token (legacy/advanced)** | Old installs may still use this shared key. The App accepts it on the local connection, never through Home Assistant Link. Run `ha-nova setup` once on each computer to switch (a `--service` install: `ha-nova pair --credential-store=file`). When every computer has switched, choose "Revoke legacy access…" on the NOVA page. New installs leave this empty: each computer gets its own key when it connects. |
| **File access (advanced)** | Lets HA NOVA read or change some Home Assistant config files. Off by default. |

The App logs in to Home Assistant by itself. Don't create or paste a Home
Assistant access token for a normal App install. A standalone Container/Core
Relay keeps its `HA_LLAT` on the server, as described in
`docs/reference/relay-container.md`.

> **Where is this page?** Home Assistant 2026.2 renamed Add-ons to Apps. This
> page is now under Settings → Apps → NOVA Relay. Setup's links work on both old
> and new versions.

### Network

| Port | Description |
|------|-------------|
| 8791/tcp | Connecting computers, and the old shared access |
| 8792/tcp | Everything a connected computer sends (encrypted, locked to this App) |

## Endpoints

You normally never call these yourself. The CLI and the NOVA page do it for
you. Connected devices use their own key over the encrypted port (8792), sent
as `Authorization: Bearer <token>`. Requests on the plain port (8791),
including `GET /health`, work only with the old shared access or a standalone
container's token, in the same header: a connected device's key is refused there.
Connecting uses the six-digit code, or the owner's Home Assistant sign-in,
instead.

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Relay health check (version, uptime, Home Assistant connection) |
| GET/POST | `/pair/v1/*` | Connect a device (OPAQUE) — the code is the key |
| POST | `/auth/device/*` | Start a device's key, or revoke it for good (encrypted port only) |
| GET | `/auth/device/peers` | Whether other devices, an open code, the old shared access, or chat-app files still use this App (encrypted port only; for `ha-nova uninstall`) |
| POST | `/ws` | Passes commands to Home Assistant's WebSocket API |
| POST | `/core` | Passes requests to Home Assistant's REST API (`/api/...`) |
| POST | `/files` | Config-file access, only when you turn it on |
| POST | `/backups` | Saved copies of configs, for undo |
| POST | `/supervisor` | Supervisor logs and Apps, from a fixed list (local ports only, not through Home Assistant Link) |

For Apps and logs, this App has the Supervisor's `manager` access, which Home
Assistant shows on this App's page. The Relay reaches only that fixed list.

Home Assistant Link and chat apps reach the Relay only from inside Home
Assistant, never on a new public port. That route list is in
[the Relay reference](https://github.com/markusleben/ha-nova/blob/main/docs/reference/bridge-architecture.md#home-assistant-link-routes).

## Setup

The easiest way to set up everything (the App, your AI app, and the skills):

- open [README.md](../README.md)
- open the [latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)
- copy the install line for your computer and run it as is

On a computer with a screen, setup opens a page in your browser. It finds your
Home Assistant. You sign in as the owner, and it installs this App for you if
needed (HA OS, Supervised). Over SSH or with `--no-browser`, it asks in the
terminal instead.

Already installed HA NOVA? Run setup again:

```bash
ha-nova setup
```

Setup finds your Home Assistant by itself. Does it find more than one? You pick
one. None? You type its address. If this App already runs (for another
computer), setup skips the install step. Then it opens your Home Assistant
login. Sign in as the owner, and this computer connects by itself. Over SSH,
as another Home Assistant user, or if sign-in fails, setup asks for a six-digit
code instead. The owner makes it on the NOVA page.

The owner connects each computer once. After that, anyone who uses the same
login on that computer (its user account, not a Home Assistant one) can use HA
NOVA; there is no separate sign-in per person. The owner is the first account
made when Home Assistant was set up. An admin with Advanced mode on (in their
Home Assistant profile) sees it under Settings → People → Users, marked Owner.
Other admins see NOVA in the sidebar too, but only the owner can open it.

Each computer gets its own key. It is saved in this computer's password store.
With no password store (for example a server), or with `--service` or
`ha-nova pair --credential-store=file`, it goes in a private file instead.
Saved old tokens, `--relay-token`, service token files, and standalone
Container/Core setups still work the old way.

### Away from home with Home Assistant Link

Do you pay for Home Assistant Link, with remote access on? Then setup can also
let this computer reach your home when you're away. On the setup page, one
switch adds Local + Home Assistant Link. In the terminal, a first setup offers:

- **Local only** (what we recommend): works at home. No Link needed.
- **Local + Home Assistant Link**: connect at home once, then add Link with the
  same key. At home, this computer still talks to Home Assistant directly. It
  uses Link only when it can't reach home.
- **Home Assistant Link only**: sign in through Link first, then connect.

Sign in to Link as the owner, and this computer connects with no code. Don't
want the owner's login saved on this computer? Sign in to Link as a standard
user instead. That only changes which login Link keeps here: the owner still
connects this computer, with a code made on the NOVA page in a separate private
browser window (setup calls it the "Owner session"). Either way, HA NOVA then
works the same for anyone who uses this login on this computer.

Your Link sign-in is saved only in this computer's password store, nowhere
else. Setup may ask you to unlock the password store. Link works on a Mac, on
Windows, and on a Linux computer with a desktop. Servers, SSH, WSL, and
containers stay at home only. Did a setup that was already checked get cut
off? You can finish it over SSH with `ha-nova cloud add --remote-resume`.

Link commands:

```bash
ha-nova cloud add [--server <name>] [--url https://<link-address>]
ha-nova cloud status [--server <name>] [--json]
ha-nova cloud unlock [--server <name>]
ha-nova cloud reconnect [--server <name>] [--url https://<link-address>]
ha-nova cloud remove [--server <name>]
ha-nova server route <local|automatic|cloud> [--server <name>]
ha-nova relay health --via <local|cloud> [--server <name>]
```

## Chat apps (Claude and ChatGPT)

With chat apps on, you can use HA NOVA from Claude (web, desktop, phone apps)
and ChatGPT (web, on a computer), without a computer running HA NOVA. They are off by default.

- **Needs:** Home Assistant 2026.10 or newer, and an address Claude and ChatGPT
  can reach: Home Assistant Link with remote access on (a subscription from
  the Home Assistant team), or your own public
  HTTPS address with a valid certificate set as Home Assistant's external URL.
- **Turn on:** NOVA page → Chat apps → "Turn on chat apps" (or during
  `ha-nova setup`, or `ha-nova chat-apps on`). Only the owner can turn them
  on. The App adds a small Home Assistant integration
  (`custom_components/ha_nova`), announces itself to it, and Home Assistant
  restarts once (about a minute).
- **Connect:** add the address the NOVA page shows in Claude ("Add to Claude")
  or ChatGPT, then sign in to Home Assistant with your own login. On a Claude Team or
  Enterprise plan, the owner of your Claude plan adds it once for everyone,
  with "Add for my organization".
  Then you click "Connect" next to HA NOVA under Customize → Connectors.
- **Who can use it:** NOVA page → Chat apps → "Who can use it". At first only
  the owner. Tick other Home Assistant users (each needs their own login) and
  pick a level: Ask only (questions), Use the home (controls lights, switches,
  fans, climate, covers, locks, the alarm, media players, vacuums, and helpers,
  and runs existing scenes, scripts, and automations; unlocking a door, opening
  a garage door or gate, and turning off the alarm need a tap on their own
  linked phone, never a chat code; can't change automations, scripts,
  dashboards, or device settings), or Manage everything (administrators only). A level never goes
  above the person's Home Assistant role. "Link phone…" in their row links
  their own phone. With anyone else on the list, going back to an older App
  refuses every chat request until you update again. To go back, first untick
  everyone but you and save. Restoring this App's backup from before the update
  brings back its settings from then: only you can use chat apps.
- **Claude asks before every step?** That's Claude, not HA NOVA: set the HA
  NOVA tools to "Always allow" (no phone linked: all but "Run a confirmed
  change").
  [How](https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md#stop-claude-from-asking-before-every-step)
- **Confirmations:** the same rules as on a computer. Unlocks, opening a
  garage door or gate, turning off the alarm, and deletes wait for a tap on the
  linked phone of whoever asked. Below Manage everything, deletes and other
  setup changes are refused, even with a phone. No phone? At
  Manage everything, a four-digit code in the chat; the AI can see that code
  too. Below that, never a code: it's refused. You can turn off codes in the chat (risky changes
  then need the phone) or let chat apps only read.
- **Another phone:** NOVA page → Chat apps → "Change phone…" or "Stop phone
  confirmations" (someone else's: "Change or stop…" in their row). The phone
  that confirms now asks first. A phone removed from
  Home Assistant can be replaced there without it.
- **A lost phone:** until you remove it in Home Assistant (Settings → Devices &
  services → Mobile App), chat apps keep asking it, for 3 minutes each time,
  and whoever holds it can answer. Then link a new phone on the NOVA page.
- **Turn off:** NOVA page → Chat apps → "Turn off chat apps". It takes effect
  at once, without a restart: the bridge stops, its own access is signed out,
  and `custom_components/ha_nova` is removed. Claude or ChatGPT hold a Home
  Assistant sign-in for your account that works for all of Home Assistant, not
  only HA NOVA, and turning chat apps off doesn't end it: to sign them out,
  remove their refresh tokens in your Home Assistant profile (Security).
- **Turn chat apps off before you remove this App or restore an older version.**
  Forgot? Install or update this App again, then run `ha-nova chat-apps off`, or choose
  "Finish turning off" on the NOVA page.

Access belongs to the Home Assistant account that signed in, not to a device:
anyone who can use that Claude or ChatGPT account can reach your home through
it. Step by step and troubleshooting:
[Use HA NOVA in Claude and ChatGPT](https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md).

## Checking Status

Open **NOVA** in the Home Assistant sidebar (see [The NOVA page](#the-nova-page))
to see the connection, updates, and devices.

On your computer, run:

```bash
ha-nova doctor
```

It checks that HA NOVA is set up, can reach the NOVA App, and that the App can
talk to Home Assistant. It also tells you when an update is ready.

Using Home Assistant Link? Run:

```bash
ha-nova cloud status --server <name>
```

It says what's wrong and which command fixes it. Set up with Link only, and
your AI app isn't ready yet? Run `ha-nova setup --server <name>`. With
`--json`, `cloud status` prints one
JSON object (`verification_error` and `next_command` when something fails). It
never prints passwords or keys.

With the old shared access or a standalone container, you can also ask the
Relay yourself (a connected computer's key doesn't work on this port):

```bash
curl \
  -H "Authorization: Bearer <relay-auth-token>" \
  http://<your-ha-ip>:8791/health
```

A healthy answer looks like this (shortened):

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "ha_ws_connected": true,
    "version": "0.13.1",
    "uptime_s": 3621
  }
}
```

## Troubleshooting

**HA NOVA can't reach the App**

- Check that this App is running (Settings → Apps → NOVA Relay). Start it if not.
- Check that nothing on your network blocks ports 8791 and 8792.
- Run `ha-nova setup` again. It checks the address and fixes it.

**The App turns this computer down**

- Run `ha-nova setup` again. It connects this computer again.
- Restart or update this App. That renews its own Home Assistant login.
- Standalone Container/Core: check that `HA_LLAT` on the server still works.

**The App isn't connected to Home Assistant yet**

- The App connects to Home Assistant only when it's first needed.
- Run `ha-nova doctor`. It makes the App connect, then says if it worked.
- Still not connected? Then look at this App's **Log** tab.

**Link says the password store is locked**

- Run `ha-nova cloud unlock --server <name>` on the computer itself, at its
  screen.
- Don't run Link commands with `sudo`, over SSH (except `ha-nova cloud add
  --remote-resume`), in WSL, as a service, or in a container.
- On Linux, you need a desktop with a password store (a Secret Service).

**Link doesn't work**

- Check that your Home Assistant Link plan and its remote access are on.
- Check that this App is installed, up to date, and running.
- Run `ha-nova cloud status --server <name>`. It says what to do next.
- It says this App or the Home Assistant user changed? Stop there. Don't work
  around it. Someone may be trying to get in.
- Did a reconnect sign in as a different Home Assistant user? HA NOVA keeps
  the old Link sign-in. Connect this computer for the right user first, then
  reconnect.
- Did you reinstall this App on purpose? With "Home Assistant Link only", run
  `ha-nova cloud remove --yes`, then `ha-nova cloud add --url ...`. With
  "Local + Home Assistant Link", run `ha-nova cloud remove`, then
  `ha-nova setup` to connect this computer again at home, then
  `ha-nova cloud add` to add Link again. Only do this after a reinstall you
  made yourself.

## Removing Home Assistant Link

`ha-nova cloud remove` signs HA NOVA out of Link and checks that it worked.
Then it deletes the Link sign-in from this computer. Your Link plan stays as it
is. This computer's connection at home stays too, unless it was set up with
Link only: then its pairing is revoked as well.

`ha-nova uninstall` asks one question:

- "Remove" signs out every Link sign-in, checks that it worked, and then
  deletes HA NOVA's keys and settings here. If it can't sign out of Link
  safely, it stops before it deletes anything. It also tries to revoke this
  computer's connection at home; if the App can't be reached, it says so, and
  you revoke the device on the NOVA page.
- "Remove, but keep the pairing for a reinstall" keeps the Home Assistant
  connection, this computer's key, and the Link sign-in.
  `ha-nova uninstall --yes` does the same; add `--purge` to remove everything.
- "Remove, and remove the NOVA App too" shows only when this App says that no
  other device, no pairing under way, no old shared access, and no chat-app
  files use it, and no Home Assistant Link is saved on this computer. The Home
  Assistant owner must sign in first.

Removing this App disconnects every computer. Turn chat apps off on the NOVA
page before you remove the App.

## Logs

The App's logs are in the **Log** tab above. Look for:

- `Relay listening (app mode)`: the Relay started.
- `Relay bootstrap (app mode)`: shows how the App logs in to Home Assistant.
- Pairing codes are **never** in the log. Make them and read them on the NOVA
  page.
- Lines with `error` or `warn` point to a problem.

## Support

- [GitHub Issues](https://github.com/markusleben/ha-nova/issues)
- [Setup Guide](https://github.com/markusleben/ha-nova#-get-started)
- [Full Documentation](https://github.com/markusleben/ha-nova)
