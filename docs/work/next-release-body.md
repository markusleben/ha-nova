# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Upgrade notes

- The standalone container (Home Assistant Container or Core) now connects with its relay token only. It no longer logs a six-digit pairing code at startup and has no code exchange; `ha-nova setup` pointed at it says so and asks for the token (`RELAY_AUTH_TOKEN`). The NOVA Relay App is unchanged: computers still pair with a sign-in or a six-digit code.
