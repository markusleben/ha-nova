# Release Version Watertight Spec

Date: 2026-03-18

## Goal

Prevent future releases where GitHub tags, skill metadata, and Claude plugin metadata drift apart.

## Problems Observed

- A hotfix release tag can be published while `version.json`, `.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json` still point to the previous version.
- Claude `/plugin` uses plugin marketplace metadata, not GitHub release tags, so version drift silently breaks the expected Claude update story.
- Non-Claude clients do not have a SessionStart hook, so update awareness must come from the skill contract itself.

## Required Fix

1. Make release metadata verification a hard scriptable gate.
2. Run that gate in local `npm run verify`, RC workflow, and final release workflow.
3. Keep Claude marketplace metadata complete and local-path based:
   - `metadata.description`
   - `metadata.version`
   - `plugins[0].version`
   - `plugins[0].source = "./"`
4. Require the base version of any release tag (`vX.Y.Z` or `vX.Y.Z-rcN`) to match `version.json`.
5. In the HA NOVA context skill, require a quiet `ha-nova check-update --quiet` on first skill use when no SessionStart update status is available.

## Why This Is The Smallest Robust Fix

- No new runtime complexity.
- No relay changes.
- No extra external service dependency.
- The release can no longer succeed with the exact metadata drift that broke Claude `/plugin`.
