# Plan: Gemini Skill Name Alignment

Date: 2026-03-17

1. Add Gemini install-name helpers so flat-copied sub-skills use namespaced names consistently.
2. Rewrite Gemini-copied sub-skill frontmatter `name:` values to the namespaced install names.
3. Rewrite Gemini-copied markdown references from `ha-nova:<subskill>` to `ha-nova:ha-nova-<subskill>`.
4. Update Gemini install tests to assert the new frontmatter/dispatch contract.
5. Run focused Go + onboarding contract tests, then full verification.
