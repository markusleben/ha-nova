# Use HA NOVA in Claude and ChatGPT

With chat apps on, you can ask about your home and change it from Claude (also on your phone) or ChatGPT on the web, on a computer. On a phone, ChatGPT can only answer. No computer has to run HA NOVA. They reach your home through Home Assistant Link or your Home Assistant's own public HTTPS address, and the same rules decide what needs your OK as with HA NOVA on a computer (see below).

## What you need

- Home Assistant OS (or Supervised) 2026.10 or newer, with the NOVA App.
- A way for Claude and ChatGPT to reach your Home Assistant over the internet, one of:
  - **Home Assistant Link** with remote access turned on (a subscription from the Home Assistant team).
  - **Your own public HTTPS address**, for example from DuckDNS, your own reverse proxy, or a Cloudflare tunnel, set as the external URL in Home Assistant (Settings → System → Network). A Tailscale address (`.ts.net`) or a local one isn't offered. It must have a valid certificate (for example from Let's Encrypt): Claude and ChatGPT refuse self-signed certificates, and they never use plain `http://`.

  No subscription needed if your Home Assistant already has a public HTTPS address; Home Assistant Link is the easiest way. When both are there, HA NOVA uses Home Assistant Link.
- You are the owner of your Home Assistant. Only the owner turns chat apps on and picks who else may use them ([Who can use it](#who-can-use-it)). When Claude or ChatGPT asks you to sign in to Home Assistant, each person signs in with their own login.
- For ChatGPT: a paid plan that lets you add your own plugins. We tested Pro. The free plan can't. At work, your admin may need to allow it.

## Turn it on

You can turn it on in either place:

- **During setup:** on "You stay in control", choose "Turn on…" next to "Use HA NOVA from your phone too" (in the browser flow: "Use HA NOVA in Claude and ChatGPT"). The page explains what happens; click "Turn on and restart Home Assistant" (just "Turn on" when Home Assistant needs no restart). No AI app on the computer? Choose "I use Claude or ChatGPT in the browser" on the first screen: right after your owner sign-in, setup checks what chat apps need (see above). If something is missing, it says what before it changes anything, with "Set it up, then check again" or "Use HA NOVA on this computer instead". Otherwise it connects the computer and shows this question first.
- **Later:** open NOVA in the Home Assistant sidebar, then Chat apps, then "Turn on chat apps". Or run `ha-nova setup` again and turn on the switch: when you click Done, you sign in to Home Assistant, then the page asks before it turns them on. Or run `ha-nova chat-apps on`.

The first time, Home Assistant restarts once, because it loads new parts only when it starts. This usually takes under a minute, longer on small devices. Setup waits for the restart and then shows an "Add to Claude" button, a link for the owner of a Claude Team or Enterprise plan ("Add for my organization"), and your connector address: your Home Assistant Link address (or your own HTTPS address) followed by `/api/ha_nova/mcp`, for example `https://abc123.ui.nabu.casa/api/ha_nova/mcp` or `https://home.example.com/api/ha_nova/mcp`. Without either address, setup says so in one line instead of the button. NOVA in Home Assistant and `ha-nova chat-apps status` show both too. Later, turning chat apps on restarts Home Assistant only when it isn't running HA NOVA, for example when you turned chat apps off and Home Assistant restarted since.

## Connect Claude

This works in Claude on the web, in the desktop app, and in the phone apps. Menu names in German are in brackets.

1. On the setup page or in NOVA in Home Assistant, choose "Add to Claude" (next to it: the address it adds). Claude opens "Add custom connector" ("Benutzerdefinierten Konnektor hinzufügen") with the name HA NOVA and your address filled in, and says the values came from a link.
   No form? In Claude, open Customize → Connectors ("Anpassungen → Konnektoren"), click + → "Add custom connector", name it HA NOVA, and paste the address.
   On a Team or Enterprise plan, members can't add it: Claude opens Connectors with no form. The owner of your Claude Team or Enterprise plan adds it once for everyone (on Enterprise, also someone whose role can manage the organization's libraries): "Add for my organization" opens Claude's organization settings → Connectors with the same values. No form there? Choose Add → Custom (→ Web), then paste the address. Then each person chooses "Connect" next to HA NOVA under Customize → Connectors and signs in with their own Home Assistant login.
2. If Claude asks how to sign in, choose "Sign in now" ("Jetzt anmelden") and "Use Claude's published identity" ("Claudes veröffentlichte Identität verwenden"). Then choose "Add".
3. Sign in to Home Assistant with your own login when Claude opens the sign-in (if it doesn't, choose "Connect"). The Claude phone app then uses it too.

## Stop Claude from asking before every step

That question comes from Claude, not from HA NOVA. At first, Claude asks before each HA NOVA step: "Allow once" or "Always allow" ("Einmal erlauben" / "Immer erlauben"). What to set depends on whether a phone is linked for chat apps (the NOVA page shows it under Chat apps, "Confirmations").

In Claude, open Customize → Connectors ("Anpassungen → Konnektoren") and choose HA NOVA. You see two groups: read-only tools ("Schreibgeschützte Tools") and write/delete tools ("Schreib-/Lösch-Tools"). Then:

- **Phone linked:** set every tool to "Always allow" ("Immer erlauben"). HA NOVA still shows you a preview before bigger changes, and your phone asks before unlocks, the alarm, and deletes. That's the same as HA NOVA on a computer.
- **No phone:** set every tool to "Always allow", except "Run a confirmed change": leave it on "Needs approval" ("Genehmigung erforderlich"). Then Claude still asks before each bigger change. The AI can see a chat code, so that question is your own check. Link a phone, and you can allow that one too.

Claude may ask again after an HA NOVA update, or now and then in a new chat. That comes from Claude: choose "Always allow" again.

## Connect ChatGPT (on a computer)

ChatGPT has no button that fills this in, so you paste the address. You need a paid plan (see "What you need"). Menu names in German are in brackets.

1. In ChatGPT on the web, on a computer, open Plugins (chatgpt.com/plugins), choose +, then "Add custom MCP server" ("Benutzerdefinierten MCP-Server hinzufügen").
2. Name it HA NOVA and paste the address under "Connection" ("Verbindung"). Pick OAuth, tick "I understand and want to continue", then choose "Create as a plugin" ("Als Plugin erstellen").
   ChatGPT says "A plugin with this name already exists"? You made one before. Open chatgpt.com/plugins, find HA NOVA under your own plugins, and choose Install. If it has another address, give the new one another name instead.
3. Sign in to Home Assistant with your own login. Right after you create the plugin, ChatGPT may say the OAuth callback data is missing ("OAuth-Callback-Daten fehlen"). Choose Connect on the plugin again.
4. Type `@HA NOVA` in a chat to use it.

After an HA NOVA update, open chatgpt.com/plugins, choose HA NOVA, then Refresh. ChatGPT then gets HA NOVA's new instructions.

Menu names in Claude and ChatGPT change now and then; look for "connector", "plugin", or "MCP".

## Use HA NOVA from your phone

- **Claude app:** once the connector is added, open the Claude app on your phone and ask, for example "Is the garage door open?" or "Close it."
- **From your computer, away from it:** if you use HA NOVA on a computer, the computer's own remote apps work too (Claude Code Remote Control, or Codex on your phone). The computer must be on. A change that needs your OK asks on the computer's window and on your linked phone at the same time; the first answer counts.

## Who can use it

At first only you, the owner. To let others in your home use HA NOVA in their own Claude or ChatGPT, open NOVA in Home Assistant, then Chat apps. Under "Who can use it", tick them, pick a level for each, and choose Save. Each person needs their own Home Assistant login (Settings → People). "How Anna joins" under a person's row says what they do.

- **Ask only:** questions about the home. Changes nothing.
- **Use the home:** controls devices (lights, switches, climate, covers, locks, the alarm, media players, and more) and runs existing scenes, scripts, and automations. Unlocks, the alarm, and other risky changes need a tap on their own linked phone, never a code in the chat. Can't change automations, scripts, dashboards, or a device's settings (like a child lock or a restart button).
- **Manage everything:** what you can do. Only for Home Assistant administrators.

A level never gives more than the person's Home Assistant role: a user gets at most Use the home, and a read-only user Ask only. Change someone's role in Home Assistant later, and their level follows it.

**Their phone:** choose "Link phone…" in their row and pick one of their phones. It must belong to them in Home Assistant (Settings → People), with its Home Assistant app signed in with their own login, and they tap "Allow" on it. Their risky changes then ask that phone, never yours.

**Removing someone** works at once, and their phone link goes too. Their chat app keeps its Home Assistant sign-in until they remove it under Profile → Security; it can no longer do anything with HA NOVA.

"Recent changes from chat apps" on the NOVA page shows the last 20 changes: who, what, and how each ended.

With anyone besides you on the list, going back to an older NOVA App refuses every chat request until you update the App again.

## What your phone confirms

- Everyday changes, like a light, run right away and are checked afterwards.
- Unlocks, the alarm, deletes, and other risky changes wait for you:
  - **With a linked phone** (the one you linked during setup), it gets a notification (also on its watch). Press and hold it on an iPhone, or pull it down on Android, then tap the button. The request stays open for up to 3 minutes. If you don't tap soon, the chat says it is still waiting; tap, then tell the assistant to go on.
  - **Without a linked phone,** the chat shows a code to type back. The AI can see this code too, so it protects less than a tap on your phone. For real protection, link your phone on the NOVA page (Chat apps, then "Link a phone…"), or on your computer (`ha-nova setup confirm-phone`) and then run `ha-nova chat-apps on` once more: it adds that phone for chat apps.
- **Another phone:** open NOVA in Home Assistant, then Chat apps, then "Change phone…", and pick the phone. The phone that confirms now asks first, then the new one. "Stop phone confirmations" asks that phone too; chat apps then show a code, or refuse risky changes when codes are off. If the phone was removed from Home Assistant, the page lets you link a new one without it. Setup offers the same move after "Use another phone".
- **A lost phone:** until you remove it in Home Assistant (Settings → Devices & services → Mobile App), chat apps refuse changes that need your OK; they don't fall back to a code. Once it's removed, link a new phone on the NOVA page (Chat apps, then "Link a new phone…").
- On the NOVA page you can also turn off codes in the chat (risky changes then need your phone), or let chat apps only read.
- If you stop setup while Home Assistant restarts, chat apps may still turn on; NOVA in Home Assistant shows whether they are on.

## Turn it off

Open NOVA in Home Assistant, then Chat apps, then "Turn off chat apps". Or run `ha-nova chat-apps off` on your computer, or run `ha-nova setup` again, turn off the switch, and click Done (you sign in to Home Assistant then). It takes effect at once; Home Assistant doesn't restart. HA NOVA stops the chat-app connection, signs its own access out, and removes the parts it added to Home Assistant. It finds them in Home Assistant itself, so this also works after a backup restore. Your choices (who can use it, the phones, codes, read only) stay for the next time.

Claude or ChatGPT hold a Home Assistant sign-in for your account that works for all of Home Assistant, not only HA NOVA, and turning chat apps off doesn't end it. To sign them out of Home Assistant, open your Home Assistant profile, then Security, and remove their entries under refresh tokens.

Turn chat apps off before you remove the NOVA App or go back to an older version.

### If you went back to an older NOVA App with chat apps on

An older NOVA App without chat apps can't remove them, so Home Assistant keeps them, and Claude and ChatGPT get an error. `ha-nova chat-apps status` says they are left over. To clean up: update the NOVA App in Home Assistant again, then run `ha-nova chat-apps off`. The updated App also cleans up by itself when it starts; if NOVA in Home Assistant still says chat apps are not fully off, choose "Finish turning off" there. Removing HA NOVA in Home Assistant's integrations doesn't help: Home Assistant adds it back until the App removes it.

To stay on the older App: removing the NOVA App clears them too, but then every computer must connect again.

## If something doesn't work

- **ChatGPT says the OAuth callback data is missing** ("OAuth-Callback-Daten fehlen") right after you create the plugin: choose Connect on the plugin again. This is a known ChatGPT issue; the second try usually works.
- **ChatGPT says "A plugin with this name already exists":** you made one before, and it may be hidden. Open chatgpt.com/plugins, find HA NOVA under your own plugins, and choose Install. If it has another address, give the new one another name instead.
- **Claude says "This connector has no tools available"** ("Dieser Konnektor hat keine Tools verfügbar"): its sign-in is gone, for example after you restored Home Assistant from a backup. In Claude, open Customize → Connectors and choose HA NOVA. Choose Disconnect ("Trennen"), then Connect ("Verbinden"), and sign in again. Still no tools? Check in NOVA in Home Assistant that chat apps are on.
- **Claude only says the connection was rejected, or ChatGPT can't connect:** you probably signed in with a Home Assistant login that isn't on the list. The owner adds it on the NOVA page (Chat apps, then "Who can use it"). Or remove the connector, add it again, and sign in with a login on the list.
- **Claude or ChatGPT can't reach your own HTTPS address:** open the address in a browser on your phone with Wi-Fi off. It must open without a certificate warning. A self-signed certificate or a port your router doesn't forward doesn't work.
- **After you turned chat apps off, Claude or ChatGPT can't connect:** that's expected. Remove HA NOVA there. Opening the address may answer "401" until Home Assistant's next restart; it no longer reaches HA NOVA.

## Good to know

- ChatGPT on a phone (its app or a phone browser) can answer questions, but it can't make changes through plugins you add yourself. For changes, use chatgpt.com on a computer, or the Claude app on your phone.
- A code in the chat protects less than your phone (see above).
- Access belongs to your Home Assistant account, not to a device: anyone who can use your Claude or ChatGPT account can reach your home through it. Remove the token in Home Assistant (above) if you lose control of that account.
- Helpers, scenes, dashboards, room and name changes, setting up integrations, and file edits work in the chat, with the same preview and confirmation as on a computer. Only setting up HA NOVA itself (pairing a computer, updates, turning chat apps on or off) happens outside the chat.
- Changes beyond controlling devices (automations, scripts, dashboards, rooms, helpers, files) need a Home Assistant administrator, as in Home Assistant itself.
- The assistant can look at a camera's current picture; it stays in your chat.
- With file access on in the NOVA Relay App, the assistant can read files in your configuration folder (never `secrets.yaml` or `.storage`) and, after you confirm the change, write or delete them. A file reaches the chat as written: a password or key typed straight into a YAML file, not into `secrets.yaml`, is seen by the AI, as it is with HA NOVA on a computer.
