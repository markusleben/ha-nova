# Windows Hidden Helper Launch Confirmation

## Summary

The first hidden `Start-Process` wrapper removed the visible console flicker on the Windows VM, but it did not reliably complete the actual `internal-uninstall` helper run. The helper launch contract now prioritizes execution certainty over fully detached wrapper startup.

## Decision

- Keep the real lifecycle helper itself backgrounded.
- Launch it through a hidden PowerShell wrapper.
- Run that wrapper to completion so helper startup is confirmed synchronously.
- Do not treat “no visible windows” as success if the runtime or PATH entry survives.

## Consequences

- Windows uninstall stays safe to close after the CLI returns.
- Wrapper launch errors surface immediately instead of failing silently in the background.
- The runtime-removal proof must check the real machine state, not just console quietness.
