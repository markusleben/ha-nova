# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Fixed in 0.31

- **Fewer phone asks in homes with a UI thermostat.** On a NOVA App with the safety check, a thermostat or hygrostat helper you made in Home Assistant's UI, in a home where an automation reacts to service calls, made everyday changes such as a light wait for your OK on the phone. The App now reads which switch the helper turns on, as HA NOVA on your computer already did, so those changes run without an ask again. Changes that need your OK still ask.
