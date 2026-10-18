# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Bug Fixes

- Windows setup over SSH (or another session without access to Credential
  Manager) now says what to do: open a normal terminal on the computer's
  desktop and run `ha-nova setup` there. The raw error stays as a detail
  line, and a localized Windows error is recognized too.
- A local-only install no longer blames Home Assistant Link when this
  computer's secure storage is unavailable; the message names this
  computer's saved credential instead.
- The Windows installer on an ARM PowerShell now lists the exact steps to
  get the x64 PowerShell (download the `-win-x64.zip`, extract, open
  `pwsh.exe`) instead of "use x64 emulation".
- On the first Windows setup, the wizard explains the Windows Firewall
  prompt raised by the network search for Home Assistant, and that either
  answer is fine.
- The setup intro and the first-look safety line match how this computer
  confirms and pairs: sign-in as the owner or a six-digit code, and "confirm
  it yourself" for unlocks instead of always promising a one-time code.
