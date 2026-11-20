# Spec: Installer TTY Handoff Without Shell Hang

Date: 2026-03-12

## Problem

The one-line installer is commonly run as `curl ... | bash`. The installer currently switches stdin globally to `/dev/tty` so interactive prompts work, but that also changes where the piped `bash` process reads after the script body ends. Result: installation finishes, then the shell keeps waiting on the terminal instead of exiting cleanly.

## Decision

Keep piped stdin untouched for the outer `bash` process.

- use `/dev/tty` only for the installer's own explicit prompt reads
- hand off the setup wizard with stdin redirected from `/dev/tty`
- keep the existing fallback error when no interactive terminal is available

## Scope

- `install.sh`
- focused installer contract coverage

## Non-Goals

- no broader onboarding refactor
- no change to the user-facing one-line install command
