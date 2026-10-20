# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## Fixes

- The setup page works for Claude or ChatGPT in the browser: choose "I use
  Claude or ChatGPT in the browser" when no AI app is on the computer, and
  step 3 shows chat apps first.
- The setup page says who the Home Assistant owner is (the first account
  made), and what to do when someone else signed in.
- On a re-run, "Use another phone" moves confirmations to a new phone. The
  phone linked now OKs the move first.
- Step 3: buttons act now ("Use this phone", "Turn on…"), switches wait for
  "Continue" (was "Finish setup"). The six-digit code is a text link, and
  the terminal screen says why setup waits there.

- Can't sign in to Home Assistant on this computer (a VM, a locked-down
  browser)? The setup page and `ha-nova chat-apps` now say how to finish
  on another device: with a six-digit code, or in NOVA in Home Assistant.
- `ha-nova uninstall` ends with a short summary: what was removed, what
  stays in Home Assistant, and one next step. File paths and links print
  only with the new `--verbose`. The keyring question and the menu are
  arrow menus (long lines wrap). When "Remove" can't run, Enter cancels.
