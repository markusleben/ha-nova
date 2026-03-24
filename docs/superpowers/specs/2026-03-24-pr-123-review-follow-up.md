## Summary

Close the two current Codex findings on PR #123 without widening scope:
- persist the Windows background-uninstall running marker before the parent-release wait
- detect `winget list` no-match via the documented WinGet exit code instead of English output text

## Changes

- write Windows uninstall recovery state before helper hand-off for both bundle and winget uninstall helpers
- add locale-independent `winget` no-match detection based on `APPINSTALLER_CLI_ERROR_NO_APPLICATIONS_FOUND`
- add regression coverage for both behaviors

## Verification

- focused Go tests for uninstall helper timing and winget exit-code handling
- `npm run verify`
