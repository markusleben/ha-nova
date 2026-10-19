# Use HA NOVA in Claude and ChatGPT

Status: active. Chat apps come with the NOVA App release that includes them; with an older NOVA App, the switch below doesn't show.

With chat apps on, you can ask about your home and change it from Claude or ChatGPT, also on your phone, with no computer running. They reach your home through Home Assistant Link, and the same rules decide what needs your OK as with HA NOVA on a computer (see below).

## What you need

- Home Assistant OS (or Supervised) 2026.10 or newer, with the NOVA App.
- Home Assistant Link, with remote access turned on.
- You are the owner of your Home Assistant. Only the owner can turn chat apps on and use them.
- For ChatGPT: a ChatGPT plan that offers developer mode.

## Turn it on

You can turn it on in either place:

- **During setup:** on "You stay in control", switch on "Use HA NOVA in Claude and ChatGPT". The page explains what happens; click "Turn on and restart Home Assistant".
- **Later:** open NOVA in the Home Assistant sidebar, then Chat apps, then "Turn on chat apps".

Home Assistant restarts once, which takes about a minute. Setup waits for it and then shows your connector address: your Home Assistant Link address followed by `/api/ha_nova/mcp`, for example `https://abc123.ui.nabu.casa/api/ha_nova/mcp`.

## Connect Claude

This works in Claude on the web, in the desktop app, and in the phone apps.

1. In Claude on the web or desktop, open Settings → Connectors.
2. Choose "Add custom connector", name it HA NOVA, paste the address, and add it.
3. Choose "Connect" and sign in to Home Assistant with your own account. The Claude phone app then uses it too.

## Connect ChatGPT (on the web)

1. In ChatGPT on the web, open Settings → Apps & Connectors → Advanced settings and turn on developer mode.
2. Back in Apps & Connectors, choose "Create", name it HA NOVA, paste the address, pick OAuth, and confirm that you trust it.
3. Sign in to Home Assistant when ChatGPT asks. In each new chat, turn it on under + → Developer mode.

Menu names in Claude and ChatGPT change now and then; look for "connector" or "app".

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

## Good to know

- ChatGPT works on the web only. Its phone app can't make changes through custom connectors.
- A code in the chat protects less than your phone (see above).
- Access belongs to your Home Assistant account, not to a device: anyone who can use your Claude or ChatGPT account can reach your home through it. Remove the token in Home Assistant (above) if you lose control of that account.
- Some changes, like helpers, dashboards, and files, still need HA NOVA on a computer. The assistant tells you when that is the case.
