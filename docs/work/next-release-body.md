# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

- **Marketplace through HA NOVA (Home Assistant 2026.11+).** HA NOVA now installs, updates, and uninstalls Marketplace items, each after your own confirmation. With the NOVA App, if Home Assistant still needs its Marketplace risk notice accepted, HA NOVA first asks you, as a step of its own, to accept it for the App's Home Assistant user: the Supervisor system user, which every App with access to Home Assistant shares. Home Assistant offers no way to withdraw that. A standalone Relay can't do this step: sign in to Home Assistant as HA NOVA's user and accept it under Settings → Marketplace (#1099).
