# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Bug Fixes

- Windows (and macOS) setup over SSH, or in an administrator terminal, now
  says what to do when secure storage can't be reached: sign in to the
  desktop of the computer HA NOVA is installed on and run `ha-nova setup` in
  a normal terminal there. The raw error stays as a detail line, a localized
  Windows error is recognized too, and `ha-nova pair` and `ha-nova doctor`
  give the same next step.
- A local-only install no longer blames Home Assistant Link when this
  computer's secure storage is unavailable; the message names this
  computer's saved credential and says how to unlock it.
- The Windows installer on an ARM PowerShell now lists the exact steps to
  get the x64 PowerShell (download the `-win-x64.zip`, extract, open
  `pwsh.exe`) instead of "use x64 emulation".
- On the first Windows setup, the wizard explains the Windows Firewall
  prompt raised by the network search for Home Assistant, and that either
  answer is fine.
- The installers, the setup intro, and the first-look safety line match how
  this computer pairs and confirms: sign-in as the owner or a six-digit code
  from the NOVA page, and "confirm it yourself" for unlocks instead of always
  promising a one-time code.

## Release-prep README edits

- README says that over SSH setup "asks for the code instead". On Windows,
  pairing over SSH can't save the credential either way: pair from the
  computer's own desktop (Credential Manager needs a desktop sign-in).
