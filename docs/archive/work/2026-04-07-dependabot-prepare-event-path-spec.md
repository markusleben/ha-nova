## Dependabot Prepare Event Path Fix

- Problem: `Dependabot Safe Lane Prepare` crashed on normal `pull_request_target` runs before the non-Dependabot guard because `EVENT_PATH` was empty.
- Root cause: the workflow relied on an injected `github.event_path` env value instead of the runner-provided `GITHUB_EVENT_PATH`, and missing-path handling was not fail-safe.
- Fix: read `GITHUB_EVENT_PATH` directly and short-circuit `should-process=false` when the event payload path is missing or unreadable.
- Scope: workflow guard only; no policy logic changes.
- Verification: workflow contract test plus PR rerun on a normal maintainer PR.
