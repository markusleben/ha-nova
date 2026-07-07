## Linux Setup Follow-Up

Date: 2026-04-17

### Scope

Fix the two live Linux setup failures found during the Hermes and private-bundle validation passes:

1. setup fell through to a misleading `homeassistant.local` fallback even though the real Linux host exposed Home Assistant via Avahi mDNS
2. setup failed with a raw Secret Service D-Bus error when secure token storage was unavailable

### Plan

1. Extend Linux Home Assistant discovery to use the real Avahi `_home-assistant._tcp` browse path.
2. Parse Avahi browse output into a usable HA host/IP without requiring the macOS `dns-sd` lookup flow.
3. Convert the specific Linux Secret Service unavailable class into a clear setup-facing recovery message.
4. Keep other secure-storage failures loud.
5. Add regression coverage and re-check behavior on the live Linux host.

### Exit Criteria

- Linux setup discovery can surface the real HA address from Avahi output.
- Setup no longer dumps the raw `org.freedesktop.secrets` error when Secret Service is unavailable.
- The new setup message tells the user what Linux prerequisite is missing and what command to rerun.
- Existing macOS `dns-sd` discovery behavior stays intact.
