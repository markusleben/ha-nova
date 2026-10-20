# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Fixes

- `ha-nova uninstall` ends with a short summary: what was removed, what
  stays in Home Assistant, and one next step. File paths and links print
  only with the new `--verbose`. The keyring question and the menu are
  arrow menus (long lines wrap). When "Remove" can't run, Enter cancels.
