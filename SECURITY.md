# Security Policy

## Supported Version

Current `main` branch is supported for security fixes.

## Security Architecture

- The Relay remains transport-only. HA NOVA runs no hosted tunnel or broker.
- Optional Home Assistant Link (formerly Home Assistant Cloud) remote access
  uses Home Assistant OAuth and Supervisor Ingress. Credential-bearing requests use only a verified canonical
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
- Chat apps (Claude and ChatGPT) are off by default. They need Home Assistant Link with remote access (a subscription from the Home Assistant team) or your own HTTPS address. Only the Home Assistant
  owner can turn them on. By default only the owner can use them, through a
  Home Assistant sign-in. The owner can add other Home Assistant users on the
  NOVA page, each at a level no higher than their Home Assistant role.
  Unlocks, deletes, and other risky changes wait for a tap on the linked phone
  of the person who asked. A code in the chat stands in for the phone only for
  people at Manage everything, and the owner can turn it off. The AI can
  see the chat code too, so a tap on the phone protects more. Claude or
  ChatGPT keep a Home Assistant sign-in for all of Home Assistant. Remove it
  in your Home Assistant profile (Security, refresh tokens).

The Cloud remote transport is a Beta. Release binaries enable it only with
signed bundle provenance; its contracts live in
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
