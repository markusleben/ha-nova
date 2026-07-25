# Security Policy

## Supported Version

Current `main` branch is supported for security fixes.

## Security Architecture

- The Relay remains transport-only. HA NOVA runs no hosted tunnel or broker.
- Optional Home Assistant Cloud remote access uses Home Assistant OAuth and
  Supervisor Ingress. Credential-bearing requests use only a verified canonical
  `https://*.ui.nabu.casa` origin and reject redirects.
- OAuth refresh tokens use a dedicated native OS credential store with no file,
  environment-variable, argument, or config fallback.
- Normal Relay calls never request secure-storage UI. Setup, reconnect, and
  explicit `ha-nova cloud unlock` are the only prompt-capable paths.
- Cloud Ingress functional routes require the genuine Supervisor peer, exactly
  one Home Assistant user identity, and an active device credential bound to
  that user and the persistent Relay instance.
- Automatic routing falls back from local to Cloud only for a pure network
  failure during a bounded authenticated preflight. TLS-pin, identity,
  authorization, and protocol failures stop before functional dispatch.
- Functional requests are never replayed across transports. Ambiguous
  completion is reported as unknown instead of retried.

The Cloud remote transport is a Beta on `main` and remains gated on the
real-device, role, lifecycle, redirect, parity, and stress matrix in
`docs/work/2026-07-25-home-assistant-cloud-remote-spec.md`.

## Reporting a Vulnerability

Please do not report security issues in public GitHub issues.

Use one of these channels:
1. GitHub private vulnerability reporting (preferred).
2. Direct maintainer contact via GitHub: `@markusleben`.

Please include:
- affected component/path
- impact
- reproduction steps
- optional proof-of-concept

Do not include live OAuth tokens, device credentials, Ingress cookies, pairing
codes, or Home Assistant access tokens. Redact secrets before attaching logs.

We will acknowledge receipt and coordinate a fix + disclosure timeline.
