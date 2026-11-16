# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## New Features

- **On macOS, 0.32 asks you to pair this computer again once.** It's one Home Assistant sign-in, about a minute, and HA NOVA's key then stays in the Keychain where only the new HA NOVA reads it. Run `ha-nova setup` when HA NOVA says so.
- **Confirm risky changes with one click on your own computer.** Unlocking, opening a garage door or gate, disarming, and deletes ask in a window on the computer you set up as your own, with a Confirm button, on macOS, Windows, and Linux. Your phone asks only when nobody confirms at the computer, and the NOVA page still works. Setup asks "Is this your own computer?" once (Enter is yes); turn it off per computer on the NOVA page. It needs the 0.32 NOVA App, and a computer paired before 0.32 pairs again once to use it.
- **Fewer asks in a home with a cover watchdog.** An automation that listens to every service call and moves covers it picks with a template no longer makes every light ask, when HA NOVA can follow everything the automation does right away (no delay or wait in it) and none of the covers it could move is a garage door, gate, or door.
- **A dashboard, automation, or script you delete from your computer asks once**, in the window, on your phone, or on the NOVA page.

## Bug Fixes

- **A change that needs your OK waits for your answer again.** Since 0.30.0, HA NOVA gave up after about 20 seconds while the NOVA page or your linked phone could still confirm it, and said it couldn't ask (nothing was sent). It now waits the whole time the ask is open. Update HA NOVA on this computer; the NOVA App needs no change.
- **Waiting no longer asks twice.** When HA NOVA stops waiting for your OK, the ask stays open for a few minutes, and running the same saved plan again picks it up instead of asking again.

## Upgrade Notes

- **On macOS, 0.32 asks you to pair this computer again once.** The first command that needs the pairing says "this computer needs to be paired again" and names the command: `ha-nova setup`, or `ha-nova setup --server <name>` for another server. It's one Home Assistant sign-in, about a minute. If an older entry of the same server is still listed in the NOVA App, setup asks once before removing it.
- **Moving HA NOVA's key into a file now turns one-click confirming off first.** `ha-nova setup --service` and `ha-nova pair --credential-store=file` ask the NOVA App to turn off one-click confirming for every key they move, because any program on this computer can read a key kept in a file. If the App can't be reached, nothing is moved and HA NOVA says why. If a pairing on this computer didn't finish, they stop and say "finish it first with: ha-nova pair --server <profile>" instead of moving its key.
- **On a Mac whose key is in the Keychain, file storage is refused.** The key is never copied into a file there. `ha-nova setup --service` keeps it in the Keychain, which HA NOVA reads without a prompt, and `ha-nova pair --credential-store=file` stops before it uses the code. A Mac with no key yet can still pair straight into a file.
- **Setup then signs the old version out of Home Assistant Link.** If this Mac used Home Assistant Link, macOS asks for your password once per old sign-in so HA NOVA can revoke it: choose "Allow", not "Always Allow". One-click confirming turns on only after Home Assistant confirms each old sign-in is revoked. If that can't finish, everything else still works, and setup names the command that finishes it later without pairing again.
- **One-click confirming trusts this computer.** On Windows and Linux, any program running as you can read HA NOVA's key and could confirm without a click; on macOS a program allowed to control the screen can click Confirm. On a computer you share or don't fully trust, answer "no" in setup or turn it off on the NOVA page.
