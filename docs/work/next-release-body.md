# Next release body

Status: active

Unreleased claims wait here until they ship (AGENTS.md, Releases: the README describes the stable release only). Each item moves into the README or the release notes only when it is in a stable release.

## Bug Fixes

- **A change that needs your OK waits for your answer again.** Since 0.30.0, HA NOVA gave up after about 20 seconds while the NOVA page or your linked phone could still confirm it, and said it couldn't ask (nothing was sent). It now waits the whole time the ask is open. Update HA NOVA on this computer; the NOVA App needs no change.

## Upgrade Notes

- **Moving HA NOVA's key into a file now turns one-click confirming off first.** `ha-nova setup --service` and `ha-nova pair --credential-store=file` ask the NOVA App to turn off one-click confirming for every key they move, because any program on this computer can read a key kept in a file. If the App can't be reached, nothing is moved and HA NOVA says why. If a pairing on this computer didn't finish, they stop and say "finish it first with: ha-nova pair --server <profile>" instead of moving its key.
