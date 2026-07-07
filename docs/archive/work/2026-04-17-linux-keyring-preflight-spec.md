# Linux keyring setup preflight

Date: 2026-04-17

## Goal

Detect Linux relay-token storage prerequisites during `ha-nova setup` before the first keyring write attempt.

## Constraints

- Keep KISS.
- No insecure token fallback.
- No prompt-heavy or hanging D-Bus unlock flow during preflight.
- Preserve existing fail-loud behavior for unexpected keyring errors.

## Plan

1. Add a Linux-only keyring preflight that:
   - checks session-bus availability,
   - calls Secret Service `ReadAlias("default")`,
   - classifies missing provider vs missing default collection,
   - avoids unlock/write operations.
2. Hook preflight into interactive setup as an early warning when no saved token exists yet.
3. Hook preflight into non-interactive setup before any token write when the token will change.
4. Add focused regression tests for:
   - interactive early warning,
   - non-interactive early failure,
   - preflight classification.
5. Live-check on the Linux SSH host.
