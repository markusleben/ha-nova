# Installing HA NOVA for Claude Code

## Quick Start

```bash
git clone https://github.com/markusleben/ha-nova.git ~/ha-nova
cd ~/ha-nova && npm install
npx ha-nova setup
```

## Activating Skills

Install the plugin via marketplace (works globally in any directory):

```
/plugin marketplace add markusleben/ha-nova
/plugin install ha-nova@markusleben/ha-nova
```

Skills are then available as `/ha-nova:read`, `/ha-nova:write`, etc.

**Alternative — per-session (development):**
```bash
claude --plugin-dir ~/ha-nova
```

## Already Set Up?

Run diagnostics:

```bash
npx ha-nova doctor
```

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
