# Mock Version Wording Design

**Date:** 2026-03-15

**Problem**

The private desktop-validation mock uses `--relay-version` and `MOCK_RELAY_VERSION` while the real project now has separate version lines:

- Relay App version in Home Assistant
- HA NOVA skill/bundle version
- Mock `/health` JSON version

That wording makes the mock look like the real HA App is being versioned by the test helper.

**Goal**

Keep the mock tiny, but make it explicit that it only reports a version string on the fake relay `/health` endpoint for validation.

**Design**

1. Rename the mock CLI argument from `--relay-version` to `--reported-version`.
2. Rename shell helper env vars from `MOCK_RELAY_VERSION` to `MOCK_REPORTED_VERSION`.
3. Change mock startup output from vague relay wording to explicit fake-health wording.
4. Add a contract test so the wording cannot drift back.

**Non-Goals**

- No change to real Relay App versioning
- No change to skill/bundle versioning
- No behavior change in the mock payload beyond wording
