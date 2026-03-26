# 2026-03-23 macOS Private RC Port-Conflict Guard

## Problem

The macOS private-RC suite started its local bundle server on `:8917` without first checking whether another local harness already owned that port.

Effect:
- the suite could silently talk to the wrong server
- installer downloads could fail with misleading `404` errors even though fresh bundles were built correctly

## Decision

Guard `BUNDLE_SERVER_PORT` with an explicit listener check before starting the suite-local `http.server`.

Keep a desktop-validation contract assertion for that guard and its operator message.

## Why

The private-RC lane should fail loudly on host-state conflicts instead of accidentally validating against whichever local server was already running.
