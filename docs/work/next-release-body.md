# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## New Features

- A room or device command that reaches exactly one matching device ("turn
  off the kitchen lights" with one light there) now runs right away and is
  verified, exactly like naming that device. Rooms with several matching
  devices and groups still show a preview first.
- **Faster setup on extra computers:** when the NOVA Relay App already runs on
  your Home Assistant, setup skips the install steps and goes straight to the
  six-digit pairing code.
- **One install link:** setup opens the NOVA Relay App page directly and adds
  the repository on the way — click "Add", then Install and Start.
- **Undo a deleted automation or script:** "undo that" right after a delete
  restores it from the copy saved before the delete, under its original
  id, with the usual preview. It never replaces an item that exists.

- **Replace a device or a phone:** "my door sensor died, I paired a new
  one" or "I have a new phone" moves automations, scripts, scenes,
  dashboards, helpers, energy sources, notifications, and presence to the new
  device in one reviewed plan. When the old device is gone for good, the new
  one takes over its entity ids, so history and statistics continue.

## What To Watch

- `ha-nova uninstall` no longer guides removal of the NOVA Relay App by
  default. It first asks whether this is the last computer using HA NOVA,
  because removing the App disconnects every paired computer.
- Pressing a button, turning on a switch, or opening a cover on a device
  that also has a lock, alarm, or access cover now needs a code. Configuration
  and diagnostic buttons there (ping, identify, restart) work as before.

## Bug Fixes

- Room and device commands now skip hidden entities and configuration or
  diagnostic entities, as Home Assistant's own room targeting does; when that
  leaves nothing, the answer names the skipped entities. A device command
  also reaches the device's child devices, as Home Assistant does.
