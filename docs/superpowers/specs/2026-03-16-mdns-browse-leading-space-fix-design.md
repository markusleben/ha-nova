# 2026-03-16 mDNS Browse Leading Space Fix

## Problem

On macOS, `dns-sd -B _home-assistant._tcp local` can emit browse rows with leading spaces before the timestamp.
The Go parser currently expects the row to start immediately with a non-space token, so valid `Add ... _home-assistant._tcp. <Instance>` lines can be missed.
When that happens, HA discovery falls back to `homeassistant.local` instead of the mDNS TXT-derived IP.

## Goal

Keep the current discovery strategy, but make the browse parser accept real `dns-sd` output with leading indentation.

## Non-Goals

- No discovery reorder
- No new network scan
- No behavior change for Windows or Linux

## Design

- Add a regression test using real `dns-sd`-style browse output with leading indentation.
- Relax the browse regex so it tolerates optional leading whitespace before the timestamp.
- Leave lookup parsing and candidate ordering unchanged.

## Success Criteria

- The regression test fails before the fix and passes after it.
- Existing discovery tests remain green.
- Real macOS discovery can recover the mDNS instance instead of falling through to `.local` fallback.
