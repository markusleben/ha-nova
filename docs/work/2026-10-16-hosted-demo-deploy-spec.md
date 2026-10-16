# Hosted demo: first deployment fixes — Spec (2026-10-16)

Status: active

## Problem

Found while preparing the first real provisioning (`docs/reference/hosted-demo.md`):

1. The Lightsail launch script never reaches `bootstrap.sh`: Lightsail puts its
   own commands in front of it under `#!/bin/sh`, and Ubuntu 24.04's dash
   rejects `set -o pipefail` (checked: dash 0.5.12, `Illegal option -o pipefail`).
2. Until DNS points at the instance, Caddy has no certificate. Only bootstrap's
   first reset skipped the public gate; every later reset failed it and took the
   house down, and the guard then rebuilt the house every minute.
3. Nothing re-checks the blocked public paths over TLS after deployment.

## Behavior

1. The launch script is POSIX sh (`set -eu`, `[ ]`).
2. `public_gate` skips (noted on stdout, so the journal shows it) while Caddy holds
   no ACME certificate for the domain; once the name resolves, it restarts Caddy so
   ACME is retried at once instead of after Caddy's backoff (up to 6 h, stops after
   30 days). `reset.sh --first-boot` is gone.
3. With a certificate, `public_gate` also requires 403 on one path per `@blocked`
   pattern in the Caddyfile; a failure takes the house down like every gate.

## Verification

Stubbed branch check of `public_gate` (no cert, no cert + DNS, internal cert only,
ACME cert, ACME cert + an unblocked path); the real run is the first provisioning.
