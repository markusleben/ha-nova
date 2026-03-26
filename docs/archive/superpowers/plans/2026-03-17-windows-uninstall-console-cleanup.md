# Windows Uninstall Console Cleanup

Date: 2026-03-17

1. Extract Windows launch profiles for visible helpers vs detached cleanup.
2. Keep the uninstall helper attached to console output, but isolate it into a new process group.
3. Run the self-delete cleanup through a detached hidden command without inherited handles.
4. Add regression tests for helper/cleanup launch behavior.
5. Re-run Go tests and full repo verification.
