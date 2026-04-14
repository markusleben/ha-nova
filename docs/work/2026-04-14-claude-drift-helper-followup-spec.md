## Goal

Close the last Codex findings on PR `#171` without widening scope.

## Changes

- Preserve pinned marketplace refs when reading structured Claude marketplace sources.
- Make Claude drift snapshots degrade cleanly when a tracked registry path is unreadable or is a directory.
- Add regression coverage for both cases.
