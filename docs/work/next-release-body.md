# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Features

- `ha-nova uninstall` asks one question: Remove (this computer's skills,
  settings, and pairing), Remove but keep the pairing for a reinstall, or
  Cancel. On the last computer using the NOVA App, it also offers to remove
  the App from Home Assistant after the owner signs in. `--yes` keeps the
  pairing unless `--purge`, and never removes the App. Release prep: update
  the `ha-nova uninstall` row in README.md ("guides removing the NOVA Relay
  App only when you say this is the last computer" no longer applies).
- The NOVA App answers `GET /auth/device/peers` to paired devices; uninstall
  offers the App removal only with an App that has it.
