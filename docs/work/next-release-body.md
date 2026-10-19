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
- Home Assistant Link setup: when you sign in as the owner, this computer
  connects without the six-digit code from a second session. Other users
  still get the code. Needs the NOVA App from this release; an older App asks
  for the code as before. Release prep: update the Cloud Remote paragraph in
  README.md ("Remote-first pairing uses a separate private Owner session").

## Fixes

- Linux: after install, `ha-nova` works in every new terminal right away
  (the PATH line now also goes to `~/.bashrc`), and the installer says how
  to use it in the current terminal. "Need help later?" prints once.
- Setup asks before it uses a Home Assistant it found on the network, also
  when it found exactly one by IP address.
- Setup in the terminal without an AI app installed still connects this
  computer, then names the AI apps to install and says to run
  `ha-nova setup` again. (The setup page keeps its "Check again" step.)
- A locked or unreachable Linux keyring gets one plain next step, with the
  system's own error as a detail line, and AI apps get a clear "keyring"
  answer instead of a connection error. A desktop terminal is no longer
  asked the service-mode question; it gets the unlock offer.
- A confirmation window closed from outside says it was closed, not that
  it timed out.
- Setup's status no longer lists "app installation" as done while the NOVA
  App isn't reachable.
