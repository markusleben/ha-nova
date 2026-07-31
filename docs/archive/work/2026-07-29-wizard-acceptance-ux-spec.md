# Wizard Acceptance UX

## Problem

A real fresh-setup rehearsal found two onboarding failures:

- ARP neighbors that merely serve HTTP are shown as reachable Home Assistant
  instances.
- When an explicit Relay address suppresses the early connection-mode choice,
  fresh setup finishes with a later command hint instead of offering the
  existing interactive Cloud fallback flow.

## Contract

- Never identify a generic HTTP server as Home Assistant.
- Prefer an explicit or saved reachable address immediately.
- Discover fresh Home Assistant instances through the official
  `_home-assistant._tcp.local.` DNS-SD service on macOS, Windows, and Linux.
- Collect every valid DNS-SD instance before selection; never let response
  order choose a home.
- If discovery finds nothing, continue to one clear manual address input.
- Keep discovery bounded and show only verified Home Assistant advertisements.
- After fresh local setup, offer the existing optional Cloud fallback flow
  whenever no earlier connection-mode choice was shown.
- After successful Cloud setup, finish with an explicit local-preferred,
  automatic-Cloud-fallback connection summary.
- Keep the setup lifecycle guard valid until the optional Cloud decision and
  persistence have finished.
- Preserve local-first routing, default-No consent, native secret storage,
  service/headless behavior, and all existing Cloud recovery gates.

## Verification

- Generic HTTP devices from the ARP table never appear.
- DNS-SD candidates require a valid Home Assistant UUID and either its
  advertised internal URL or official UUID host/port fallback.
- Saved/explicit reachable addresses do not wait for network enumeration.
- A fresh constrained Wizard run reaches
  `Add Home Assistant Cloud fallback now? [y/N]`.
- Declining finishes locally; accepting reuses the paired device and enters the
  existing OAuth flow.
- Targeted tests pass on macOS plus Windows and Linux compile checks.
- One isolated real Wizard rehearsal completes without Census or production
  configuration changes.
