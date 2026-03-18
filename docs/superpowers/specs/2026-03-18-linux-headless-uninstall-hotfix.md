# Linux Headless Uninstall Hotfix

Date: 2026-03-18
Status: implemented

## Problem

`v0.2.0` published successfully, but the Ubuntu post-publish smoke job failed during `ha-nova uninstall --yes`.

Observed failure:

- install succeeded
- `ha-nova version` succeeded
- `ha-nova check-update --quiet` succeeded
- `ha-nova update --version <same-version>` succeeded
- uninstall removed files, then exited with `failed to remove relay auth token`

Root cause:

- on headless Ubuntu runners there is no desktop Secret Service session
- Linux keyring reads therefore return `org.freedesktop.secrets ... not provided`
- uninstall treated that read failure as fatal, even though no usable desktop token store existed to clean up

## Decision

Keep uninstall strict for real token failures, but tolerate the specific “desktop keyring unavailable” read path.

Rules:

- missing token: still non-fatal
- desktop keyring unavailable on read: non-fatal, add an uninstall note
- other read failures: still fatal
- delete failures after a successful read: still fatal

## Verification

- add regression coverage for headless Secret Service read failure
- add regression coverage that generic keyring read failures still fail loud
- rerun targeted CLI uninstall tests
- rerun full Go CLI tests
