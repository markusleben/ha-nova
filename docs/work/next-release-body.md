# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

- **Marketplace through HA NOVA (Home Assistant 2026.11+).** HA NOVA now installs, updates, and uninstalls Marketplace items, each after your own confirmation. With the NOVA App, if Home Assistant still needs its Marketplace risk notice accepted, HA NOVA first asks you, as a step of its own, to accept it for the App's Home Assistant user: the Supervisor system user, which every App with access to Home Assistant shares. Home Assistant offers no way to withdraw that. A standalone Relay can't do this step: sign in to Home Assistant as HA NOVA's user and accept it under Settings → Marketplace (#1099).
- **Which integration is failing, without reading the log.** HA NOVA now reads log facts: for each integration of your home, how many errors and warnings Home Assistant's log still holds and since when. It never reads the log's lines, which can hold passwords and addresses; for those it still asks you to look under Settings → System → Logs. Needs the NOVA App 0.31.

## Fixed in 0.31

- **Fewer phone asks in homes with a UI thermostat.** On a NOVA App with the safety check, a thermostat or hygrostat helper you made in Home Assistant's UI, in a home where an automation reacts to service calls, made everyday changes such as a light wait for your OK on the phone. The App now reads which switch the helper turns on, as HA NOVA on your computer already did, so those changes run without an ask again. Changes that need your OK still ask.

## Changed in 0.31

- **Fixing a repair and changing an integration's sub-entries now ask for your OK.** What such a fix or change does is up to the integration (it may remove devices or entities), so HA NOVA asks before each step, on every route, also when an AI app sends the request directly. Renaming a device's entity onto the id of one it replaces, and moving an entity, device, or room into an area, floor, or label, ask too when an automation triggered there does something that needs your OK, such as unlocking.
