# Installing HA NOVA for Claude Code

## Quick Start (Plugin)

```bash
git clone https://github.com/markusleben/ha-nova.git ~/ha-nova
cd ~/ha-nova && npm install
npx ha-nova setup
```

Claude Code automatically discovers skills via the plugin system (`.claude-plugin/plugin.json`).
No manual skill installation needed.

## Already Set Up?

Run diagnostics:

```bash
npx ha-nova doctor
```

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
