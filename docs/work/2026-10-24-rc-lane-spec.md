# One-command release-candidate check and test-bed phone lane (2026-10-24)

Status: active

## Why

Each release candidate cost an agent about an hour of hand work: the client
matrix from several versions, headless chat-bridge runs through a proxy that
drops server instructions, fake phones with a capture server for push
payloads, and the slow-check setup with 1,500 automations. The Android bug
(#842: `authenticationRequired` as a bool broke every Android link push) was
found by a user, not by a test: no test looked at what Home Assistant really
sends to the push relay.

## 1. Test bed phone lane (`scripts/e2e/testbed/phone.mjs`)

Opt-in: `TESTBED_PHONE=1 npm run testbed:verify`, or `node scripts/e2e/testbed/phone.mjs`
on a running bed. The weekly `testbed-ha-stable.yml` job runs it.

Fixture (once per bed; `.state/phone.json` marks it done):
- `push`: a compose service on the pinned Home Assistant image (no extra
  pull) running `push.py`, a fake Companion push relay. Home Assistant's
  `mobile_app` notify posts there instead of to a real phone. It keeps the
  bodies in memory and serves them on `127.0.0.1:18792` (loopback only).
- Two admin users with random passwords (never printed or stored):
  `Testbed Relay` (the Relay signs in as it, like the App's own user; the
  owner's token makes phone confirmation unavailable by design) and
  `Phone Tester` (with a person entity: the person who confirms).
- Two fake `mobile_app` phones registered as `Phone Tester`:
  `Testbed iPhone` (maker Apple) and `Testbed Pixel` (maker Google), each
  with `push_url` `http://push:8080/push/<ios|android>`.
- The Relay container is recreated with `Testbed Relay`'s token.

A "tap" is what the Companion app does: `fire_event`
`mobile_app_notification_action` on the phone's own webhook, so the event
carries the phone user's context and origin `REMOTE`.

Run (own CLI home `.state/phone-home`, `confirm_window: off`, never the
verify home): link iPhone → confirm an unlock on iPhone → move to Pixel →
confirm an unlock on Pixel → move back to iPhone → stop on iPhone → link
Pixel → stop on Pixel. That covers link, move, stop, confirm and result on
both platforms.

Assertions per question push:
- it reaches only the phone being asked (path), title and button as the CLI
  defines them, second button `Cancel`, ids `HANOVA_(OK|NO)_<32 hex>`;
- link, move, stop: `authenticationRequired` is the bool `true` on iOS and
  the string `"true"` on Android; a plan confirm has none;
- Android: every action value is a string (except `destructive`, which the
  relay drops); `importance: high`;
- per-platform hint: iOS message ends "Press and hold to answer.", Android
  message starts "Pull down to answer."; the CLI's "Sent to …" line and a
  plan's `say` use the same platform's tap line;
- `channel: HA NOVA`, `priority: high`, positive `ttl` and `timeout`,
  interruption level `time-sensitive` for a plan, `active` otherwise;
- each ask has its own tag (`ha-nova-<link|setting|plan id>-<8 hex>`), never
  reused.

Per result push: same tag as its question (it replaces exactly that one), no
buttons, `importance: high`, `interruption-level: active`, the expected title.

`TESTBED_CLI_BIN` runs a given binary instead of the checkout's build
(`cli.sh` honors it, and `TESTBED_CLI_HOME` for a separate home).

## 2. `npm run rc-lane -- <tag>` (`scripts/e2e/rc-lane.mjs`)

Runs in order, prints one summary table, exits non-zero on any FAIL:

| Step | What |
|---|---|
| a | client matrix `--from <last stable>` and `--from v0.24.0`, `--to <tag>`, lanes `macos,linux` |
| b | phone lane with the tag's published binary for this host |
| c | chat bridge: `ha-nova serve --server demo` over HTTP, a proxy that strips `instructions` (as claude.ai), headless Claude with only HA NOVA tools and `ha_apply` denied; EN + DE × unlock + batch of two; the code(s) must be the last lines. SKIP when `claude` or `$HOME/.claude-ha-nova-test` is missing |
| d | slow check (#827): 1,500 filler automations + one `call_service` listener; a lock unlock plan must hit the read budget and still apply with its first code (`verified`) |

Safety and cleanup:
- It starts its own test bed from this checkout and fails fast when any
  `ha-nova-testbed` is running (from any tree): never reuse, never tear down
  someone else's. The matrix now refuses a bed from another tree too (it
  reused a stale one before).
- Downloads only the tag's binary (checksum-verified against
  `checksums.txt`); every CLI home is a throwaway under `.state` or the work
  dir with the file test keyring, so the host keychain is never read; the
  Linux and macOS matrix lanes keep their own guards.
- At exit: bed down (when it started it), bridge and proxy stopped, work dir
  removed (`--keep` keeps it). Logs of failed steps are printed before removal.

## 3. Runbook

`docs/releasing.md` §4: `rc-lane` is the standard RC check after the RC tag's
`release.yml` is green. The Cloud checklist and real phones stay manual for
the maintainer.

## Limits

- The bed's Relay is built from the checkout, not from the tag: run rc-lane
  from a checkout at the tag (or newer with the same Relay version).
- Fake phones show what Home Assistant sends to the push relay, not what the
  FCM/APNs relay or a real phone does with it.
- Chat check: one run per prompt, Sonnet; a model can still vary run to run.
