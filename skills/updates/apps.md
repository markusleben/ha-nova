# Apps (Supervisor)

Read on demand: `ha-nova:updates` for the App store, installs, removals, settings, and auto-update; `ha-nova:diagnose` for App, Core, Supervisor, and host logs. Needs the NOVA Relay App on Home Assistant OS or Supervised. A `404` / `NOT_FOUND` (a standalone container, or an App older than this route) means it is not available: say so, offer the App update (`ha-nova:updates`), never work around it. Over Home Assistant Link the CLI refuses it: run it at home.

## Reads

`ha-nova relay supervisor -d '{"method":"GET","path":"<path>"}' --jq <filter>`; a log adds `"lines":<1-1000>` (default 100, newest last). Chat apps: `ha_read` with endpoint `supervisor`, `path`, `lines`. The answer: `.data.status` and `.data.body`, the Supervisor's `{result, data}` (a log: plain text).

| Need | Path |
|---|---|
| Installed Apps (slug, name, state, version, update_available) | `/addons` |
| One App: options, schema, state, boot, auto_update | `/addons/<slug>/info` |
| Store | `/store`, `/store/addons`, `/store/addons/<slug>` |
| System | `/info`, `/os/info`, `/host/info`, `/core/info`, `/supervisor/info`, `/resolution/info` |
| Network, hardware, storage | `/network/info`, `/hardware/info`, `/host/disks/default/usage`, `/os/datadisk/list` |
| Logs | `/addons/<slug>/logs`, `/core/logs`, `/supervisor/logs`, `/host/logs`, `/host/logs/identifiers/<id>` (ids: `/host/logs/identifiers`; boots: `/host/logs/boots`) |

Nothing else is reachable; a refusal (exit 4) is final: name the Home Assistant page instead (Settings > Apps, Settings > System). Values that look like passwords or keys come back as `(hidden: …)`. Logs are not filtered: quote only the lines that matter, never a secret that shows up in one.

Resolve a slug by name from `/addons` (installed) or `/store/addons`; never guess one. HA NOVA's own App (slug containing `ha_nova_relay`) is refused for every change.

## Changes

Always `ha-nova plan raw` then `ha-nova apply` (chat apps: `ha_plan` kind raw, `ha_apply`), one item: `{"endpoint":"supervisor","payload":{"method":"POST","path":"<path>","data":{…}}}`.

| Change | Path | data | Confirmation |
|---|---|---|---|
| Install | `/store/addons/<slug>/install` | none | one-time code |
| Uninstall | `/addons/<slug>/uninstall` | `{"remove_config":true}` only when the user wants its settings deleted too | one-time code |
| Settings | `/addons/<slug>/options` | `{"options":{<only the changed keys>}}` | one-time code |
| Auto-update | `/addons/<slug>/options` | `{"auto_update":true\|false}` | preview |

- **Install**: preview the name, version, description, and what it may reach from `/store/addons/<slug>` (`host_network`, `full_access`, `privileged`, `hassio_role`, `homeassistant_api`, `docker_api`; say each one plainly). It installs in the background: poll `/store/addons/<slug>` until `installed` is true (every ~10 s, up to 10 minutes), else say it is still running. It does not start the App: offer `hassio.addon_start` (`ha-nova:service-call`).
- **Uninstall**: from `/addons/<slug>/info`, say what stops with it (an MQTT broker's or Z-Wave controller's devices, a database's history). Verify it is gone from `/addons`.
- **Settings**: read `/addons/<slug>/info` first and check the new values against its `schema`. Send only the changed keys: the Relay keeps every other stored option, hidden ones included. Never send a hidden value (refused). A list or object option is replaced whole; when it holds a hidden value, send the user to the App's Configuration tab instead. The App reads its settings on start: offer `hassio.addon_restart` (`ha-nova:service-call`). Verify by re-reading.
- **Auto-update**: verify `auto_update` in `/addons/<slug>/info`.
- Elsewhere: start, stop, restart (`ha-nova:service-call`); App updates (`ha-nova:updates`); backups (`ha-nova:backup`); repositories, DNS, network, host, OS, and Supervisor settings stay in the Home Assistant UI.
