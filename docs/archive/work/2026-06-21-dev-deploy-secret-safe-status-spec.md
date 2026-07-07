# Dev Deploy Secret-Safe Status Spec

Status: active

Date: 2026-06-21

## Problem

`npm run deploy:app` prints `ha apps info <slug>` as its quick status. On the live HA host this includes App `options`, including `relay_auth_token` and `ha_llat`.

## Change

- Keep the quick status, but read `ha apps info --raw-json` and print only non-secret fields.
- Never print `options`, `relay_auth_token`, or `ha_llat` from the deploy helper.
- Keep options save/restore behavior unchanged.

## Verification

- Deploy-script contract test rejects the unsafe plain `ha apps info` status call.
- Bash syntax check for the deploy helper.
