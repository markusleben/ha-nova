# 2026-03-23 Winget Validation Warning Cleanup

## Summary

Remove avoidable `winget validate` warnings from the generated HA NOVA manifest before opening the public `winget-pkgs` PR.

## Required Changes

- Add YAML schema headers to all generated manifest files
- Drop installer fields that the current portable package shape does not support cleanly
- Keep the generated manifest sourced from the tagged Windows bundle asset
- Re-run staged submission + real Windows `winget validate`

## Acceptance

- Generated manifest files include the matching `yaml-language-server` schema comment
- Portable installer manifest no longer emits the `Scope is not supported for InstallerType portable` warning
- Real Windows validation is re-run against the updated staged manifest
- Release docs/helper/tests treat warning-free `winget validate` as the expected pre-PR outcome, not just "validate ran"
