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

Your AI app (Claude Code, Claude Desktop, Codex, OpenCode, Google Antigravity,
Hermes Agent) talks to the Relay with its own key. Each computer gets its key
when it connects, so you never copy or paste one. The know-how lives in the AI
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
  read, or turn off codes in the chat (see [Chat apps](#chat-apps-claude-and-chatgpt)).

Only the Home Assistant owner can open this page. It opens only from inside
Home Assistant, never by its network address.

## Configuration

| Option | Description |
|--------|-------------|
| **Relay Auth Token (legacy/advanced)** | Old installs may still use this shared key. HA NOVA 0.27 is the last release that accepts it. Run `ha-nova setup` once on each computer to switch (a `--service` install: `ha-nova pair --credential-store=file`). When every computer has switched, choose "Revoke legacy access…" on the NOVA page. New installs leave this empty. The App then makes and keeps its own private one. |
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
including `GET /health` and the old shared access, use the same header.
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

Each computer gets its own key. It is saved in this computer's password store.
With no password store (for example a server), or with `--service` or
`ha-nova pair --credential-store=file`, it goes in a private file instead.
Saved old tokens, `--relay-token`, service token files, and standalone
Container/Core setups still work the old way.

### Away from home with Home Assistant Link

Do you pay for Home Assistant Link, with remote access on? Then setup can also
let this computer reach your home when you're away. It offers:

- **Local only** (what we recommend): works at home. No Link needed.
- **Local + Home Assistant Link**: connect at home once, then add Link with the
  same key. At home, this computer still talks to Home Assistant directly. It
  uses Link only when it can't reach home.
- **Home Assistant Link only**: sign in through Link first, then connect.

Sign in as the owner, and this computer connects with no code. Don't want the
owner's login saved on this computer? Sign in as a standard user instead. HA
NOVA can still do the same things. Then the owner makes a code on the NOVA
page, in a separate private browser window (setup calls it the "Owner
session").

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
  or ChatGPT, then sign in to Home Assistant as the owner. On a Claude Team or
  Enterprise plan, the owner of your Claude plan adds it once for everyone,
  with "Add for my organization".
  Then you click "Connect" next to HA NOVA under Customize → Connectors.
- **Confirmations:** the same rules as on a computer. Unlocks, the alarm, and
  deletes wait for a tap on the owner's linked phone. No phone? The owner types
  a four-digit code in the chat; the AI can see that code too. You can turn off codes in the chat (risky changes
  then need the phone) or let chat apps only read.
- **Turn off:** NOVA page → Chat apps → "Turn off chat apps". It takes effect
  at once, without a restart: the bridge stops, its own access is signed out,
  and `custom_components/ha_nova` is removed. Claude or ChatGPT hold a Home
  Assistant sign-in for your account that works for all of Home Assistant, not
  only HA NOVA, and turning chat apps off doesn't end it: to sign them out,
  remove their refresh tokens in your Home Assistant profile (Security).
- **Turn chat apps off before you remove this App or restore an older version.**
  Forgot? Update this App again, then run `ha-nova chat-apps off`, or choose
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

You can also ask the Relay yourself:

```bash
curl \
  -H "Authorization: Bearer <relay-auth-token>" \
  http://<your-ha-ip>:8791/health
```

A healthy answer looks like this:

```json
{
  "ok": true,
  "data": {
    "status": "ok",
    "ha_ws_connected": true,
    "version": "0.11.4",
    "uptime_s": 3621
  }
}
```

## Troubleshooting

**HA NOVA can't reach the App**

- Check that this App is running (Settings → Apps → NOVA Relay). Start it if not.
- Check that nothing on your network blocks port 8791.
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
- Don't run Link commands with `sudo`, over SSH, in WSL, as a service, or in a
  container.
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
Then it deletes the Link sign-in from this computer. Your Link plan and this
computer's connection at home stay as they are.

`ha-nova uninstall` asks one question:

- "Remove" signs out every Link sign-in and this computer's connection, checks
  that it worked, and then deletes HA NOVA's keys and settings here. If it
  can't sign out safely, it stops before it deletes anything.
- "Remove, but keep the pairing" keeps the Home Assistant connection, this
  computer's key, and the Link sign-in, for a later reinstall.
  `ha-nova uninstall --yes` does the same; add `--purge` to remove everything.
- "Remove, and remove the NOVA App too" shows only when this App says that no
  other device, no old shared access, and no chat-app files use it. The Home
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
