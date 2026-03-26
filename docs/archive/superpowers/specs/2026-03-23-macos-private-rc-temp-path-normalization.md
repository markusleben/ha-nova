# 2026-03-23 macOS Private RC Temp-Path Normalization

## Problem

The macOS private-RC helpers used raw `mktemp` output as `TMP_HOME`.

On this host, `TMPDIR` already ended with `/`, so the resulting shell path could contain `//`.
Claude's plugin metadata normalized the same path to a single-slash form.

Effect:
- helper assertions could fail even when Claude was attached correctly
- the failure showed up as a false negative in the Claude per-client lane

## Decision

Normalize `TMP_HOME` with `os.path.normpath(...)` immediately after `mktemp` in the macOS private-RC helpers.

Keep a desktop-validation contract assertion so the normalization helper remains present.

## Why

The private-RC lane should compare semantic paths, not host-specific slash quirks from temp-dir construction.
