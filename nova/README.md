# NOVA Relay

The bridge between your AI assistant and Home Assistant — the server-side half of [HA NOVA](https://github.com/markusleben/ha-nova).

**What it does:** Your AI app on your computer (Claude Code, Codex, OpenCode, Google Antigravity, …) talks to this relay through the `ha-nova` command, and the relay passes those calls to Home Assistant. Your Home Assistant access token stays here on the server — it never reaches your computer and never appears in an AI conversation.

**Chat apps:** with chat apps on, you can also use HA NOVA in Claude (web, desktop, and phone apps) and ChatGPT (web), with no computer running. They need Home Assistant Link with remote access (a subscription from the Home Assistant team) or your own HTTPS address, and Home Assistant 2026.10 or newer. Open **NOVA** in the sidebar, then **Chat apps**. Off by default; only the owner can turn them on. Guide: [Use HA NOVA in Claude and ChatGPT](https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md).

**What it deliberately is not:** smart. The relay carries no Home Assistant domain logic, no caching, no interpretation — a handful of generic endpoints, small enough to read in one sitting. All the intelligence lives in plain, readable skill files on your computer.

Good to know:

- Setup happens on your computer with one command. It opens a setup page in your browser: you sign in as the owner, and it installs this App for you if needed. Start at the [HA NOVA README](https://github.com/markusleben/ha-nova#readme).
- Relay configuration, health checks, logs, and troubleshooting live in the **Documentation** tab of this App ([`nova/DOCS.md`](https://github.com/markusleben/ha-nova/blob/main/nova/DOCS.md)).
- The `file_access` option is **off by default**: the relay cannot touch your configuration files unless you opt in here.

<!-- Governance: this file is intentionally only a pointer — product truth lives
in the root README.md (public product/install/support view), relay/operator
truth in nova/DOCS.md (Home Assistant App / relay setup, endpoints, health,
logs, troubleshooting). Do not duplicate endpoints, ports, versions, or other
operational detail here. -->
