# Wave 7 Update UX Spec

Status: merged — #362 and #363; released in v0.18.0 on 2026-07-16
Date: 2026-07-15
Sequencing SSOT: `docs/work/masterplan-2026-h2.md` -> Wave 7
Release train: HA NOVA 0.18.0 with Relay App 0.6.0

## Goal

Make update discovery and completion equally clear in every supported AI client, then publish the completed H2 waves through the normal release gates.

## Current truth

Update discovery parity already landed before this wave:

- `skills/ha-nova/SKILL.md` runs `ha-nova check-update --quiet` before the first HA task in every client session.
- Relay `ws` and `core` traffic surfaces a cached, throttled update notice for every client; `relay health` is the unthrottled diagnostic path.
- Claude additionally has a SessionStart path. Codex, OpenCode, Antigravity, and Hermes install overlays document the shared first-use/Relay path, and onboarding contracts pin it.

No second set of per-client hooks will be added. Current clients expose different hook/plugin formats and trust controls, while the existing skill/Relay mechanism already covers all supported clients without mutating their general hook configuration.

The remaining behavior gap is after a successful update: the CLI reports the installed version, but the required new-client-session action is easy to miss or exists only in agent guidance.

## Delivery

### 1. Post-update session nudge

- After a successful bundle replacement and client sync, print one explicit final line: start a new AI client session to load the updated HA NOVA skills.
- Print the same line after an already-current run successfully re-syncs client integrations.
- On Windows, the foreground staging message says to wait for the updater; the background replacement helper prints the final session instruction only after replacement and client sync succeed.
- Never print success guidance after staging, replacement, rollback, or client-sync failure.
- Keep the wording client-neutral; do not tell users to restart an entire desktop app when a new chat/session is sufficient.

### 2. Update-notice parity verification

- Retain the existing universal first-use check and Relay-traffic nudge as the SSOT.
- Add or tighten contracts only where needed to prove all five client overlays and the context skill describe the same update action and new-session requirement.
- Do not install hooks, plugins, or client-global configuration solely for update notices.

### 3. Release-prep README entry point

- Replace the README's "open the latest release and copy a command" steps with visible macOS/Linux and Windows one-liners.
- Keep commands versionless in README. The installer selects the latest stable release; concrete versions remain in `version.json`, release metadata, and tag-pinned release notes.
- The README edit lands only in the release-prep PR with the skill-version bump, Relay App 0.6.0 bump, short user-facing release notes, and required manifest/README labels.
- Keep `min_relay_version` at its existing floor unless the release audit finds a hard skill-runtime dependency. Pairing and snapshot features retain their explicit legacy/capability fallbacks.

## PR boundaries

1. Spec: this document plus masterplan status.
2. Runtime UX: CLI post-update session nudge, focused Go tests, update guide, and client-overlay contracts.
3. Release prep: HA NOVA 0.18.0 manifests, Relay App 0.6.0 metadata/changelog, README one-liners, and concise release notes.
4. Post-release closeout: archive the release-body draft and mark Wave 7/program status complete.

Each PR completes the repository merge checklist independently. The release-prep PR is release-bound and requires a real clean Codex result on its final SHA.

## Release gates

- Audit open PRs first and classify them as `blocker now` or `separate later`.
- Run the strict release pipeline audit and dispatched disposable-HA E2E on the exact release commit.
- An RC rehearsal is required because the release contains installer, Go CLI, onboarding, and Relay delivery changes.
- Publish the final tag only from the reviewed release-prep merge commit, then verify the final release workflow and a clean public install.

## Acceptance

- Every successful update/sync path ends with a clear new-session instruction; failure paths do not.
- All five supported client paths retain automatic or first-use update discovery without client-specific hook installation.
- README shows both supported install one-liners without a concrete version claim.
- Release-prep checks, strict audit, disposable-HA E2E, RC rehearsal, final publish, and public-install verification pass on the required commits.
- Every PR receives a real clean Codex result on its final SHA and has all review threads resolved before merge.

## Research basis

- Codex lifecycle hooks and managed-hook policy: <https://github.com/openai/codex/blob/main/docs/config.md>
- OpenCode plugin hooks: <https://opencode.ai/docs/plugins/>
- Google Antigravity hooks: <https://antigravity.google/docs/hooks>
- Hermes plugin and shell hooks: <https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/hooks.md>
