# Client Integration

HA NOVA clients are registered in `clients/registry.json`.

The registry is metadata and capability truth. Install behavior still lives in explicit client adapter code; do not add a registry-only client and expect setup/install to work without adapter coverage.

## Minimal Client

```json
{
  "id": "future",
  "label": "Future Client",
  "adapter_kind": "skill_tree",
  "supported_os": ["macos", "linux"]
}
```

## Service Credentials

Some clients can run from a service, gateway, SSH, or headless session where the desktop keyring is not unlocked. Those clients can opt in to a setup prompt:

```json
{
  "id": "hermes",
  "label": "Hermes Agent",
  "adapter_kind": "skill_tree",
  "supported_os": ["macos", "linux"],
  "setup": {
    "service_credentials": {
      "recommended_when": ["headless", "locked_keyring", "systemd_user_service"],
      "label": "Service / gateway mode",
      "help": "Use this when the client runs without an unlocked desktop keyring."
    }
  }
}
```

This enables `ha-nova setup --service <client>` and lets interactive setup offer a service token file when the selected client declares the capability. The setup flow must stay generic: do not hardcode a Hermes-only branch for this prompt.

The service token file stores only the Relay Auth Token. It does not store the Home Assistant Long-Lived Access Token.

## OS Overrides

Use `per_os` when a setup capability differs by platform:

```json
{
  "per_os": {
    "windows": {
      "setup": {
        "service_credentials": {
          "disabled": true
        }
      }
    }
  }
}
```

Keep registry metadata declarative. Do not add cleanup globs, shell hooks, or dynamic scripts to client entries.
