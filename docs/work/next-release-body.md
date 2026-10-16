# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## New Features

- A room or device command that reaches exactly one matching device ("turn
  off the kitchen lights" with one light there) now runs right away and is
  verified, exactly like naming that device. Rooms with several matching
  devices, groups, and devices that also carry a lock, alarm, or gate still
  show a preview first.
- **Faster setup on extra computers:** when the NOVA Relay App already runs on
  your Home Assistant, setup skips the install steps and goes straight to the
  six-digit pairing code.
- **One install link:** setup opens the NOVA Relay App page directly and adds
  the repository on the way — click "Add", then Install and Start.

## What To Watch

- `ha-nova uninstall` no longer guides removal of the NOVA Relay App by
  default. It first asks whether this is the last computer using HA NOVA,
  because removing the App disconnects every paired computer.
