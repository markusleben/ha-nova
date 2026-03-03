# Publish-Readiness: Onboarding Wizard Design

Date: 2026-03-02
Status: Approved

## Goal

Make ha-nova publishable on GitHub with a frictionless onboarding experience for HA beginners with AI interest. One command (`npx ha-nova setup`) guides the user through everything.

## Target Audience

HA-Einsteiger mit AI-Interesse. Kennt HA grundlegend, braucht aber Erklärungen für LLAT, Add-on Repositories, Relay-Konzept.

## Approach: Hybrid Wizard with Deep-Links

Automate everything possible (TCP probe, config storage, Keychain, token generation, skill installation). Guide the user through browser-based steps (App install, LLAT creation) using `my.home-assistant.io` deep-links — the established HA ecosystem standard.

### Research Findings

| Operation | Automatable? | Reason |
|---|---|---|
| App install | No | Supervisor API only accessible internally (HA design) |
| Custom repo add | No | Same restriction |
| LLAT creation | Theoretically (OAuth2+WS) | Too complex for MVP |
| Relay reachability | Yes | TCP probe + health check |
| Keychain + config | Yes | Local macOS operations |
| Skill installation | Yes | npm scripts |

Sources: HA Supervisor API docs, HA Core `hassio/http.py` path filtering, HACS installation pattern, `my.home-assistant.io` redirect spec.

## Wizard Flow (4 Phases)

### Phase 1: Prerequisites Check (automatic, ~5s)

```
$ npx ha-nova setup

  HA NOVA Setup
  ─────────────
  Checking prerequisites...
  ✓ macOS detected
  ✓ Node.js 20+
  ✓ npm available
```

- Checks: OS (macOS only), Node version, npm
- On failure: clear message + abort

### Phase 2: App Installation (guided, deep-links)

```
  Step 1/4 — Install NOVA Relay in Home Assistant

  I'll open your browser to add our repository to Home Assistant.
  Press [Enter] to open...

  [opens: https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https://github.com/markusleben/ha-nova]

  In Home Assistant:
  1. Confirm adding the repository
  2. Go to Add-on Store → search "NOVA Relay"
  3. Click Install → wait for completion → Start

  Press [Enter] when the add-on is running...
```

- `open` (macOS) opens browser automatically
- Deep-link leads directly to repo-add page
- Wizard waits for user confirmation

### Phase 3: Token Setup (guided + semi-automated)

```
  Step 2/4 — Configure Authentication

  a) Relay Token
     Generated a secure token for you: <random-32-char>

     I'll open the NOVA Relay add-on configuration.
     Press [Enter] to open...

     [opens: http://<ha-host>:8123/hassio/addon/local_ha_nova_relay/config]

     Paste this token into "relay_auth_token" and click Save.
     Press [Enter] when saved...

  b) Home Assistant Access Token (LLAT)
     I'll open your HA profile page.
     Press [Enter] to open...

     [opens: https://my.home-assistant.io/redirect/profile/]

     1. Scroll to "Long-Lived Access Tokens"
     2. Create Token → name it "NOVA"
     3. Copy the token
     4. Go back to NOVA Relay add-on config
     5. Paste into "ha_llat" field → Save → Restart add-on

     Press [Enter] when done...
```

- Relay token auto-generated (`openssl rand -hex 16`)
- LLAT: deep-link to profile, clear instructions
- Relay token stored in macOS Keychain (never in prompt)

### Phase 4: Verification + Finish (automatic)

```
  Step 3/4 — Verifying connection...

  ? Enter your Home Assistant address
    (detected: http://192.168.1.5:8123) [Enter to confirm]

  ✓ Relay reachable at http://192.168.1.5:8791
  ✓ Authentication valid
  ✓ WebSocket connected to Home Assistant
  ✓ Config saved to ~/.config/ha-nova/
  ✓ Token stored in macOS Keychain

  Step 4/4 — Installing skills...
  ✓ Skills installed for Claude Code

  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Setup complete!

  Try asking: "List my automations"
  Run diagnostics: npx ha-nova doctor
  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

- HA host auto-detection (existing logic from macos-setup.sh)
- Relay health + WS ping validation
- Retry loop on failure (with clear error messages)
- Skill installation automatic

## README Redesign

Radically minimal landing page (~30 lines):

```markdown
# HA NOVA

AI-powered Home Assistant control. One command to set up, then talk to your home.

> macOS only. Requires Home Assistant OS or Supervised.

## Quick Start

\```bash
npx ha-nova setup
\```

The setup wizard guides you through everything:
installing the relay, configuring tokens, and connecting your AI client.

## What is HA NOVA?

HA NOVA lets AI assistants (Claude, Codex, OpenCode) control your Home Assistant.
It works through a small relay app on your HA instance that bridges
AI clients to your smart home — securely, with no cloud dependency.

## After Setup

Ask your AI assistant things like:
- "List my automations"
- "Turn off the living room lights"
- "Create an automation that turns on the porch light at sunset"

## Troubleshooting

\```bash
npx ha-nova doctor
\```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
```

## CLI Entry Point

Bash-based thin router in `bin/ha-nova`:

```bash
#!/usr/bin/env bash
case "${1:-}" in
  setup)   exec bash "$(dirname "$0")/../scripts/onboarding/macos-setup.sh" ;;
  doctor)  exec bash "$(dirname "$0")/../scripts/onboarding/macos-doctor.sh" ;;
  *)       echo "Usage: ha-nova <setup|doctor>" ;;
esac
```

Plus `package.json` `"bin"` field for `npx` support.

## Platform Abstraction

Platform-specific functions isolated for future Linux/Windows support:

```
scripts/onboarding/
├── setup-flow.sh         # Platform-independent wizard logic
├── doctor-flow.sh        # Platform-independent doctor logic
├── platform/
│   ├── macos.sh          # open_browser, store_token, read_token
│   ├── linux.sh          # (future)
│   └── windows.ps1       # (future)
└── lib/
    ├── ui.sh             # Print/prompt helpers (platform-independent)
    └── relay.sh          # Relay health/ws checks (platform-independent)
```

Now: only `macos.sh` implemented. `setup-flow.sh` sources the right platform file based on `uname`. Adding Linux = add `linux.sh`, no rewrite needed.

## Script Refactoring Scope

### Extends existing:
- HA host auto-detection (ARP scan, mDNS, manual input)
- Keychain token storage
- Config file management (`~/.config/ha-nova/onboarding.env`)
- Relay CLI wrapper installation
- Doctor checks

### New:
1. Prerequisites check (OS, Node, npm)
2. App installation guide with deep-links
3. Relay token auto-generation (`openssl rand -hex 16`)
4. LLAT guide with deep-link to profile
5. Automatic skill installation at end
6. Better UX (step numbering, retry loops, clear error messages)

### Deprecated:
- `.claude/INSTALL.md` → 3-liner pointing to `npx ha-nova setup`
- `.codex/INSTALL.md` → same
- `npm run onboarding:macos` → alias, calls new flow
- Legacy `.legacy-backup.*` skill directories → cleanup with `trash`

## Error Handling

Every error follows: **What happened → Why → What to do → Retry**

| Error | Message Pattern |
|---|---|
| Relay unreachable | "Can't reach relay. Is the add-on running? Check HA → Settings → Add-ons. [Enter] to retry" |
| Token mismatch (401) | "Token mismatch. The relay_auth_token in the add-on must match. [Enter] to re-enter" |
| HA unreachable | "Can't reach Home Assistant. Check your network. Enter a different address or [Enter] to retry" |
| WS not connected | "WebSocket not connected. Is ha_llat set in add-on config? [Enter] after fixing" |

## YAGNI (Explicitly NOT doing)

- No architecture diagram in README
- No glossary document
- No screenshots (text-based, screenshots get stale)
- No Windows/Linux implementation now (only abstraction prep)
- No OAuth2 flow (post-MVP)
- No HACS integration
- No CI/CD for app publishing (manual for now)

## Success Criteria

1. New user runs `npx ha-nova setup` and has working setup in <10 minutes
2. Every failure state has a clear next-step message
3. `npx ha-nova doctor` validates the full chain
4. README is <40 lines
5. Zero manual file editing required (no .env, no config files)
