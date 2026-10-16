# Hosted demo: first deployment fixes — Spec (2026-10-16)

Status: active

## Problem

Found while preparing the first real provisioning (`docs/reference/hosted-demo.md`):

1. The Lightsail launch script never reaches `bootstrap.sh`: Lightsail puts its
   own commands in front of it under `#!/bin/sh`, and Ubuntu 24.04's dash
   rejects `set -o pipefail` (checked: dash 0.5.12, `Illegal option -o pipefail`).
2. Until DNS points at the instance, nothing is served over TLS. Only bootstrap's
   first reset skipped the public gate; every later reset failed it and took the
   house down, and the guard then rebuilt the house every minute.
3. Nothing re-checks the blocked public paths over TLS after deployment.
4. Review findings on the same paths: `reset.sh` could exit before hiding the
   house (missing relay image, unreadable digest file); the egress probes counted a
   missing tool as "blocked"; first-boot races (static IP attach, apt lock,
   `/run/sshd` absent under socket activation) could stop the launch script or
   bootstrap; a bootstrap re-run could race a timer reset.

## Behavior

1. The launch script is POSIX sh and retries git install + clone for ~5 min.
2. `public_gate` returns 2 (skipped, noted on stdout, `$PENDING` set) only while
   nothing at all is served over TLS on the host; anything served, whatever the
   certificate, runs the verifying gate and fails closed. `reset.sh` accepts 0 or 2.
3. `reset.sh` restarts Caddy (house hidden, lock held, before `up.sh`) when the name
   resolves to this instance (IMDSv2) and nothing is served over TLS. `guard.sh`
   starts one reset when `$PENDING` is set and TLS is served.
4. With TLS, the gate also requires 403 on every `@blocked` pattern, read from the
   Caddyfile.
5. `update-ha.sh` tries no candidate while nothing is served over TLS, and holds the
   lock for its own gates; a skipped public gate never promotes a candidate.
6. `reset.sh` hides the house first; its early checks go through `gate_failed`.
   `down.sh` wipes `.state` with `TESTBED_WIPE_IMAGE` (the demo passes Caddy's
   pinned image, never pruned). `egress_gate` adds positive controls.
7. `bootstrap.sh`: `/run/sshd` before `sshd -t`, apt retries, env written only after
   the relay image exists, timers paused on re-runs (restarted on failure).

## Verification

Stubbed branch check of `public_gate` (no TLS, TLS untrusted, TLS verified with all
blocked patterns, one pattern unblocked); the real run is the first provisioning.
