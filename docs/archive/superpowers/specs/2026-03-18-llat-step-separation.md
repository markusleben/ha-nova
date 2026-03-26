# 2026-03-18: LLAT Step Separation

## Problem

The setup wizard handled Relay Auth Token and Home Assistant Access Token guidance inside one mixed step.
That made the secure-access phase feel crowded and obscured which token the user was working on.

## Goals

- Split the Home Assistant Access Token walkthrough into its own wizard step.
- Keep the Relay Auth Token visible once more inside the LLAT step as a reminder only.
- Preserve truthful skip behavior for reuse/recovery flows that do not need the LLAT walkthrough.
- Keep back-navigation predictable between Relay token, LLAT, and verify steps.

## Approach

- Add a dedicated LLAT setup stage and render it as its own step.
- Rename the Relay token page so the current task is obvious.
- Compute wizard step numbers from whether the LLAT step is present in the active path.
- Update interactive setup tests to cover the new copy and reminder behavior.
