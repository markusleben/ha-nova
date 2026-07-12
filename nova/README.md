# NOVA Relay

The bridge between your AI assistant and Home Assistant — the server-side half of [HA NOVA](https://github.com/markusleben/ha-nova).

**What it does:** Your AI client (Claude Code, Codex, OpenCode, Google Antigravity, …) talks to this relay through the `ha-nova` CLI, and the relay forwards those calls to Home Assistant. Your Home Assistant access token stays here on the server — it never reaches your laptop and never appears in an AI conversation.

**What it deliberately is not:** smart. The relay carries no Home Assistant domain logic, no caching, no interpretation — a handful of generic endpoints, small enough to read in one sitting. All the intelligence lives in plain, readable skill files on your computer.

Good to know:

- Setup happens on your computer with one command — start at the [HA NOVA README](https://github.com/markusleben/ha-nova#readme).
- Relay configuration, health checks, logs, and troubleshooting live in the **Documentation** tab of this App ([`nova/DOCS.md`](https://github.com/markusleben/ha-nova/blob/main/nova/DOCS.md)).
- The `file_access` option is **off by default**: the relay cannot touch your configuration files unless you opt in here.

<!-- Governance: this file is intentionally only a pointer — product truth lives
in the root README.md (public product/install/support view), relay/operator
truth in nova/DOCS.md (Home Assistant App / relay setup, endpoints, health,
logs, troubleshooting). Do not duplicate endpoints, ports, versions, or other
operational detail here. -->
