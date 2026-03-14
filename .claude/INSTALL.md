# Installing HA NOVA for Claude Code

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
```

Choose `Claude Code` when prompted.

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
ha-nova doctor
```

## Related

- Codex: `.codex/INSTALL.md`
- OpenCode: `.opencode/INSTALL.md`
- Gemini CLI: `.gemini/INSTALL.md`
