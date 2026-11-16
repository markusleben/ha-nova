# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Bug Fixes

- **A change that needs your OK waits for your answer again.** Since 0.30.0, HA NOVA gave up after about 20 seconds while the NOVA page or your linked phone could still confirm it, and said it couldn't ask (nothing was sent). It now waits the whole time the ask is open. Update HA NOVA on this computer; the NOVA App needs no change.

## Upgrade Notes

- **Moving HA NOVA's key into a file now turns one-click confirming off first.** `ha-nova setup --service` and `ha-nova pair --credential-store=file` ask the NOVA App to turn off one-click confirming for every key they move, because any program on this computer can read a key kept in a file. If the App can't be reached, nothing is moved and HA NOVA says why. If a pairing on this computer didn't finish, they stop and say "finish it first with: ha-nova pair --server <profile>" instead of moving its key.
- **On a Mac whose key is in the Keychain, file storage is refused.** The key is never copied into a file there. `ha-nova setup --service` keeps it in the Keychain, which HA NOVA reads without a prompt, and `ha-nova pair --credential-store=file` stops before it uses the code. A Mac with no key yet can still pair straight into a file.
- **After the update, a Mac pairs again once.** The first command that needs the pairing says "this computer needs to be paired again" and names the command: `ha-nova setup`, or `ha-nova setup --server <name>` for another server. It's one Home Assistant sign-in, about a minute. If an older entry of the same server is still listed in the NOVA App, setup asks once before removing it.
- **Setup then signs the old version out of Home Assistant Link.** If this Mac used Home Assistant Link, macOS asks for your password once per old sign-in so HA NOVA can revoke it: choose "Allow", not "Always Allow". One-click confirming turns on only after Home Assistant confirms each old sign-in is revoked. If that can't finish, everything else still works, and setup names the command that finishes it later without pairing again.
