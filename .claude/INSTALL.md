# Installing HA NOVA for Claude Code

## Quick Start

```bash
git clone https://github.com/markusleben/ha-nova.git ~/ha-nova
cd ~/ha-nova && npm install
npx ha-nova setup
```

## Activating Skills

Claude Code requires explicit plugin registration. Choose one:

**Option A — Per-session (development):**
```bash
claude --plugin-dir ~/ha-nova
```

**Option B — Persistent (recommended):**

Add to `~/.claude/settings.json`:
```json
{
  "extraKnownMarketplaces": {
    "ha-nova-local": {
      "source": { "source": "directory", "path": "/absolute/path/to/ha-nova" }
    }
  },
  "enabledPlugins": {
    "ha-nova@ha-nova-local": true
  }
}
```

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, etc.

## Already Set Up?

Run diagnostics:

```bash
npx ha-nova doctor
```

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
