# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Bug Fixes

- A YAML generic thermostat or hygrostat no longer makes every service-call automation count for every call. With the App's file access on, HA NOVA reads the YAML Home Assistant runs from `/config`, including a YAML template switch as the heater, so a light beside such a thermostat and an automation that moves covers by template asks nothing again. If a file changed since Home Assistant started, the plan says which thermostat it couldn't read; a restart clears it. Needs this release's NOVA App.

## Upgrade notes

- The standalone container (Home Assistant Container or Core) now connects with its relay token only. It no longer logs a six-digit pairing code at startup and has no code exchange; `ha-nova setup` pointed at it says so and asks for the token (`RELAY_AUTH_TOKEN`). The NOVA Relay App is unchanged: computers still pair with a sign-in or a six-digit code.
