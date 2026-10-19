# Use HA NOVA in Claude and ChatGPT

Status: active. Chat apps come with the NOVA App release that includes them; with an older NOVA App, the switch below doesn't show.

With chat apps on, you can ask about your home and change it from Claude or ChatGPT, also on your phone, with no computer running. They reach your home through Home Assistant Link or your Home Assistant's own public HTTPS address, and the same rules decide what needs your OK as with HA NOVA on a computer (see below).

## What you need

- Home Assistant OS (or Supervised) 2026.10 or newer, with the NOVA App.
- A way for Claude and ChatGPT to reach your Home Assistant over the internet, one of:
  - **Home Assistant Link** with remote access turned on.
  - **Your own public HTTPS address**, for example from DuckDNS, your own reverse proxy, or a Cloudflare tunnel, set as the external URL in Home Assistant (Settings → System → Network). A Tailscale address (`.ts.net`) or a local one isn't offered. It must have a valid certificate (for example from Let's Encrypt): Claude and ChatGPT refuse self-signed certificates, and they never use plain `http://`.

  No subscription needed if your Home Assistant already has a public HTTPS address; Home Assistant Link is the easiest way. When both are there, HA NOVA uses Home Assistant Link.
- You are the owner of your Home Assistant. Only the owner can turn chat apps on and use them. When Claude or ChatGPT asks you to sign in to Home Assistant, sign in as the owner, not as another Home Assistant user.
- For ChatGPT: a plan that lets you add your own MCP server. It worked with ChatGPT Pro in our test; your plan's and workspace's rules apply.

## Turn it on

You can turn it on in either place:

- **During setup:** on "You stay in control", switch on "Use HA NOVA in Claude and ChatGPT". The page explains what happens; click "Turn on and restart Home Assistant".
- **Later:** open NOVA in the Home Assistant sidebar, then Chat apps, then "Turn on chat apps".

Home Assistant restarts once, which takes about a minute. Setup waits for it and then shows an "Add to Claude" button and your connector address: your Home Assistant Link address (or your own HTTPS address) followed by `/api/ha_nova/mcp`, for example `https://abc123.ui.nabu.casa/api/ha_nova/mcp` or `https://home.example.com/api/ha_nova/mcp`. Without either address, setup says so in one line instead of the switch. NOVA in Home Assistant and `ha-nova chat-apps status` show both too.

## Connect Claude

This works in Claude on the web, in the desktop app, and in the phone apps. Menu names in German are in brackets.

1. On the setup page or in NOVA in Home Assistant, choose "Add to Claude" (next to it: the address it adds). Claude opens "Add custom connector" ("Benutzerdefinierten Konnektor hinzufügen") with the name HA NOVA and your address filled in, and says the values came from a link.
   Without the button: in Claude on the web or desktop, open Settings → Connectors, choose "Add custom connector", name it HA NOVA, and paste the address.
2. If Claude asks how to sign in, choose "Sign in now" ("Jetzt anmelden") and "Use Claude's published identity" ("Claudes veröffentlichte Identität verwenden"). Then choose "Add".
3. Sign in to Home Assistant as its owner when Claude opens the sign-in (if it doesn't, choose "Connect"). The Claude phone app then uses it too.

## Connect ChatGPT (on the web)

ChatGPT has no button that fills this in, so you paste the address. Menu names in German are in brackets.

1. In ChatGPT on the web, open Plugins (chatgpt.com/plugins), choose +, then "Add custom MCP server" ("Benutzerdefinierten MCP-Server hinzufügen").
2. Name it HA NOVA and paste the address under "Connection" ("Verbindung"). Pick OAuth, tick "I understand and want to continue", then choose "Create as a plugin" ("Als Plugin erstellen").
3. Sign in to Home Assistant as its owner. Then type `@HA NOVA` in a chat to use it.

Menu names in Claude and ChatGPT change now and then; look for "connector", "plugin", or "MCP".

## Use HA NOVA from your phone

- **Claude app:** once the connector is added, open the Claude app on your phone and ask, for example "Is the garage door open?" or "Close it."
- **From your computer, away from it:** if you use HA NOVA on a computer, the computer's own remote apps work too (Claude Code Remote Control, or Codex on your phone). The computer must be on. A change that needs your OK asks on the computer's window and on your linked phone at the same time; the first answer counts.

## What your phone confirms

- Everyday changes, like a light, run right away and are checked afterwards.
- Unlocking a door, deleting, and other risky changes wait for you:
  - **With a linked phone** (the one you linked during setup), it gets a notification (also on its watch). Tap to allow. If you don't tap within about half a minute, the chat says it is still waiting; tap, then tell the assistant to go on.
  - **Without a linked phone,** the chat shows a code to type back. The AI can see this code too, so it protects less than a tap on your phone. For real protection, link your phone on your computer (`ha-nova setup confirm-phone`), then run `ha-nova chat-apps on` once more: it adds that phone for chat apps.
- On the NOVA page you can also turn off codes in the chat (risky changes then need your phone), or let chat apps only read.
- If you stop setup while Home Assistant restarts, chat apps may still turn on; NOVA in Home Assistant shows whether they are on.

## Turn it off

Open NOVA in Home Assistant, then Chat apps, then "Turn off chat apps". It takes effect at once; Home Assistant doesn't restart. HA NOVA stops the chat-app connection, signs its own access out, and removes the part it added to Home Assistant. Your choices on the NOVA page (phone, codes, read only) stay for the next time.

To also sign Claude or ChatGPT out of Home Assistant, open your Home Assistant profile, then Security, and remove their entries under refresh tokens.

Turn chat apps off before you remove the NOVA App.

## If something doesn't work

- **ChatGPT says the OAuth callback data is missing** ("OAuth-Callback-Daten fehlen"): connect once more. This comes from ChatGPT's sign-in window, and a second try usually works.
- **Claude only says the connection was rejected, or ChatGPT can't connect:** you probably signed in to Home Assistant as another user. Only the owner can use HA NOVA in chat apps. Remove the connector, add it again, and sign in as the owner.
- **Claude or ChatGPT can't reach your own HTTPS address:** open the address in a browser on your phone with Wi-Fi off. It must open without a certificate warning. A self-signed certificate or a port your router doesn't forward doesn't work.
- **After you turned chat apps off, Claude or ChatGPT can't connect:** that's expected. Remove HA NOVA there. Opening the address may answer "401" until Home Assistant's next restart; it no longer reaches HA NOVA.

## Good to know

- ChatGPT works on the web only. Its phone app can't make changes through plugins you add yourself.
- A code in the chat protects less than your phone (see above).
- Access belongs to your Home Assistant account, not to a device: anyone who can use your Claude or ChatGPT account can reach your home through it. Remove the token in Home Assistant (above) if you lose control of that account.
- Some changes, like helpers, dashboards, and files, still need HA NOVA on a computer. The assistant tells you when that is the case.
