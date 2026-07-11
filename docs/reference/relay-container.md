# NOVA Relay as a standalone container

The NOVA Relay ships two ways from **one** codebase:

| Home Assistant install | How to run the relay |
|------------------------|----------------------|
| **HA OS / Supervised** | The **NOVA Relay App** (Settings > Apps). `ha-nova setup` walks you through it. |
| **HA Container / HA Core** | This **standalone container** — those installs have no Supervisor, so they cannot install Apps. |

Same source, same three endpoints, same version line (`nova/config.yaml`). There is no second implementation to drift.

## Run it

```bash
docker run -d --name ha-nova-relay --restart unless-stopped \
  -p 8791:8791 \
  -e RELAY_AUTH_TOKEN='<your relay token>' \
  -e HA_LLAT='<your Home Assistant long-lived access token>' \
  -e HA_URL='http://<home-assistant-host>:8123' \
  ghcr.io/markusleben/ha-nova-relay:latest
```

Docker Compose:

```yaml
services:
  ha-nova-relay:
    image: ghcr.io/markusleben/ha-nova-relay:latest
    container_name: ha-nova-relay
    restart: unless-stopped
    ports:
      - "8791:8791"
    environment:
      RELAY_AUTH_TOKEN: "<your relay token>"
      HA_LLAT: "<your Home Assistant long-lived access token>"
      HA_URL: "http://homeassistant:8123"
```

If Home Assistant runs in the same Compose project, `HA_URL` can use its service name; otherwise use the host's IP.

## Environment

| Variable | Required | Default | Meaning |
|----------|----------|---------|---------|
| `RELAY_AUTH_TOKEN` | yes | — | Shared secret between the `ha-nova` CLI and the relay. Any long random secret (see Setup order). |
| `HA_LLAT` | yes | — | Home Assistant long-lived access token. **Stays on the server** — the AI client never sees it. |
| `HA_URL` | no | `http://homeassistant:8123` | Where Home Assistant lives. |
| `RELAY_PORT` | no | `8791` | Listen port. |
| `RELAY_VERSION` | no | baked in | Version reported by `/health`. The published image bakes it in at build time — do not set it yourself. |

## Setup order

The interactive wizard is built around the Supervisor App (it walks you through the App store and its configuration screens), so a Container/Core install uses the **non-interactive** setup path instead — it is the same CLI, just told where things already are:

1. **Pick a relay token** (any long random secret; this is the shared secret between your machine and the relay):
   ```bash
   openssl rand -hex 32
   ```
2. **Create a long-lived access token** in Home Assistant: Profile > Security > "Create Token" (name it `NOVA`).
3. **Start the container** with both (see the run/compose examples above).
4. **Attach the CLI**, pointing it at your Home Assistant and the relay:
   ```bash
   ha-nova setup --non-interactive \
                 --ha-url http://<home-assistant-host>:8123 \
                 --relay-url http://<docker-host>:8791 \
                 --relay-token '<the same relay token>'
   ```
   `--non-interactive` is required: without it the CLI runs the interactive wizard, which walks you through the Supervisor App screens you do not have.
   The CLI stores the relay token in your OS keychain, verifies the connection, and installs the skills for your AI clients.
5. **Check it**: `ha-nova doctor`.

A guided "Docker" branch in the interactive wizard is planned; until then this is the supported path, and it is the one the documentation and tests cover.

The security model is identical to the App: the LLAT lives only on the server side (in the container's environment), and the relay token is stored in your OS keychain — the AI client never sees your Home Assistant token.

## Notes

- **No Supervisor watchdog.** Use `--restart unless-stopped` (or Compose's `restart:`) so the relay comes back after a reboot.
- **Updating**: pull the new image and **recreate** the container — `docker restart` would keep running the old image:
  ```bash
  docker pull ghcr.io/markusleben/ha-nova-relay:latest
  docker rm -f ha-nova-relay
  # then run it again with the same command as above
  ```
  With Compose: `docker compose pull && docker compose up -d`.
  The CLI warns during normal skill use when the running relay is below the version the installed skills require (`version.json:min_relay_version`).
- **Process isolation** is the same benefit the App has: a relay crash cannot take Home Assistant down, and it holds one scoped token rather than running inside the HA process.
- The image runs as a non-root user and exposes only the relay port.

## Local build

```bash
docker build -f nova/Dockerfile.standalone --build-arg RELAY_VERSION=0.3.0 -t ha-nova-relay:local nova
```

The published image bakes its version in at build time (`RELAY_VERSION`), so `/health` reports a real version and the CLI's compatibility check works. A local build without `--build-arg` reports `dev`.
