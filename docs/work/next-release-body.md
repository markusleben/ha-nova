# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Fixes

- Can't sign in to Home Assistant on this computer (a VM, a locked-down
  browser)? The setup page and `ha-nova chat-apps` now say how to finish
  on another device: with a six-digit code, or in NOVA in Home Assistant.
- `ha-nova uninstall` ends with a short summary: what was removed, what
  stays in Home Assistant, and one next step. File paths and links print
  only with the new `--verbose`. The keyring question and the menu are
  arrow menus (long lines wrap). When "Remove" can't run, Enter cancels.
