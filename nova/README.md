# NOVA Relay

AI-powered Home Assistant control through LLM coding agents.

![Supports aarch64 Architecture][aarch64-shield]
![Supports amd64 Architecture][amd64-shield]

## About

Thin relay that connects AI coding clients (Claude Code, Codex, Gemini CLI, OpenCode)
to Home Assistant. All intelligence lives in Markdown skills — the relay provides
secure, authenticated API access.

- Persistent WebSocket connection
- macOS Keychain token isolation
- Zero business logic (~1.5K LOC)

[![Full documentation →][docs-shield]][docs]

[aarch64-shield]: https://img.shields.io/badge/aarch64-yes-green.svg
[amd64-shield]: https://img.shields.io/badge/amd64-yes-green.svg
[docs-shield]: https://img.shields.io/badge/docs-GitHub-blue.svg
[docs]: https://github.com/markusleben/ha-nova
