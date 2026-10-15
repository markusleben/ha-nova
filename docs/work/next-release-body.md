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
