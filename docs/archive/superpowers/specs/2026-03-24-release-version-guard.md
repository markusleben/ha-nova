# 2026-03-24: Release Version Guard

## Scope

- Prevent future release work from accidentally reusing an already published GitHub version.

## Decisions

- Add a dedicated `verify-next-release-version` script that checks the target version against the latest published stable GitHub release.
- Use the same script in:
  - manual maintainer preflight
  - final release workflow
  - RC publish workflow
- Fail if:
  - the exact tag already exists on GitHub
  - the target base version is not newer than the latest published stable version

## Reason

- Release metadata sync alone is not enough.
- We also need a remote-truth gate against what is already live on GitHub.
