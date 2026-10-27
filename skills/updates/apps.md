# Apps (Supervisor)

Read on demand: `ha-nova:updates` for the App store, installs, removals, settings, and auto-update; `ha-nova:diagnose` for App, Core, Supervisor, and host logs. Needs the NOVA Relay App on Home Assistant OS or Supervised.

## Reads

`ha-nova relay supervisor --data-file <payload-file> --jq <filter>` with `{"method":"GET","path":"<path>"}`; a log adds `"lines":<1-1000>` (default 100, newest last). Chat apps: `ha_read` with endpoint `supervisor`, `path`, `lines`. The answer: `.data.status` and `.data.body`, the Supervisor's `{result, data}` (a log: plain text).

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

Errors: the Relay's own `NOT_FOUND` (`.ok` false) means no Supervisor route here (a standalone container, or an App older than this route): say so and offer the App update (`ha-nova:updates`). Over Home Assistant Link it is refused: connect from home. A Supervisor `404` (`.data.status`) means a wrong slug: resolve it again. A Supervisor `403` means the App lacks its new access: restart the App, then read again. `SUPERVISOR_SELF_UNREADABLE` and `SUPERVISOR_OPTIONS_UNREADABLE` sent nothing; `SUPERVISOR_OUTCOME_UNKNOWN` may still finish: read the state again before saying anything failed.

## Changes

Always `ha-nova plan raw` then `ha-nova apply` (chat apps: `ha_plan` kind raw, `ha_apply`), one item: `{"endpoint":"supervisor","payload":{"method":"POST","path":"<path>","data":{…}}}`.

| Change | Path | data | Confirmation |
|---|---|---|---|
| Install | `/store/addons/<slug>/install` | none | one-time code |
| Uninstall | `/addons/<slug>/uninstall` | none, or `{"remove_config":true}` to delete its config folder too | one-time code |
| Settings | `/addons/<slug>/options` | `{"options":{<only the changed keys>}}` | one-time code |
| Auto-update | `/addons/<slug>/options` | `{"auto_update":true\|false}` | preview |

- **Install**: check `available` in `/store/addons/<slug>`, then preview the name, version, description, its security `rating` (1-6, higher is safer), and what it may reach (`host_network`, `host_pid`, `full_access`, `apparmor`, `hassio_role`, `homeassistant_api`, `auth_api`, `docker_api`; say each one plainly). It installs in the background: poll `/store/addons/<slug>` until `installed` is true (every ~10 s, up to 10 minutes); if it never turns true, read `/supervisor/logs` (50 lines) for the reason. It does not start the App: offer `hassio.addon_start` (`ha-nova:service-call`).
- **Uninstall**: always deletes the App's settings and data (`remove_config` adds its config folder). Say so, and what stops with it (an MQTT broker's or Z-Wave controller's devices, a database's history), from `/addons/<slug>/info`; offer a backup with the App first (`ha-nova:backup`) when the user may want it back. Verify it is gone from `/addons`.
- **Settings**: read `/addons/<slug>/info` first and check the new values against its `schema`. Send only the changed keys: the Relay keeps every other stored option, hidden ones included. Refused: a hidden value, a `!secret` reference, and a list or object option that holds a hidden value (send the user to the App's Configuration tab). The App reads its settings on start: offer `hassio.addon_restart` (`ha-nova:service-call`). Verify by re-reading; a new secret reads back hidden, so never repeat it to the user.
- **Auto-update**: verify `auto_update` in `/addons/<slug>/info`.
- Elsewhere: start, stop, restart (`ha-nova:service-call`); App updates (`ha-nova:updates`); backups (`ha-nova:backup`); repositories, DNS, network, host, OS, and Supervisor settings stay in the Home Assistant UI.
