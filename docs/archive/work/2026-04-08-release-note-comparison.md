# Release Note Comparison

Date: 2026-04-13
Mode: provisional stable preview

## Compare Context

- Baseline release: `v0.3.2`
- Baseline published at: `2026-04-03T11:57:50Z`
- Baseline commit: `89f9af2`
- Target ref: `main`
- Target commit: `1132ce7`
- Compare window: `v0.3.2..1132ce7`
- HEAD freeze at classification start: `1132ce7`
- HEAD freeze at finalization: `1132ce7`
- Target selection note: no published RC, no newer non-`main` release target, and no separate release branch with a different tree

## Delta Classification Matrix

| Candidate | Classification | Primary files | Include | Section | Rationale |
|---|---|---|---|---|---|
| Dedicated `dashboard` skill for storage dashboards, Lovelace resources, and targeted card changes | New Feature | `skills/dashboard/SKILL.md`, `README.md`, `skills/ha-nova/SKILL.md`, `skills/fallback/SKILL.md` | yes | `New Features` | This is a new named supported workflow with safer dashboard handling than the old generic fallback path. |
| Dedicated `organize` skill for areas, floors, labels, categories, and richer entity/device metadata | New Feature | `skills/organize/SKILL.md`, `README.md`, `skills/ha-nova/SKILL.md`, `skills/fallback/SKILL.md` | yes | `New Features` | This exposes organization and registry metadata work as a first-class supported workflow. |
| Dedicated `history` skill for bounded history, logbook timelines, and long-term statistics | New Feature | `skills/history/SKILL.md`, `README.md`, `skills/ha-nova/SKILL.md`, `skills/fallback/SKILL.md` | yes | `New Features` | This gives history and trend questions a dedicated read-only supported workflow instead of generic fallback handling. |
| Gemini auto-discovers shipped subskills and syncs the newly promoted skills without hardcoded wiring | Install & Update Behavior | `cli/client_gemini.go`, `scripts/onboarding/lib/install-local-skills-common.sh` | yes | `New Features` | Supported-client behavior changes for Gemini users because the new skills now ship through install and sync automatically. |
| Bundled `axios` bump to `1.15.0` in root and `nova` closes critical shipped production audit findings | Bug Fix | `package.json`, `package-lock.json`, `nova/package.json`, `nova/package-lock.json` | yes | `Bug Fixes` | This removes critical security issues from both shipped production manifests and was important enough to merge into the release lane before tagging. |
| Routing ownership moves dashboard / organize / history out of generic `fallback` | release-note bullet folded into primary features | `skills/ha-nova/SKILL.md`, `skills/fallback/SKILL.md` | no | folded | This is user-visible, but it is better expressed inside the three promoted skill bullets than as a separate bullet. |
| Stable install guidance in `nova/DOCS.md` points readers to the latest release instead of `main` | intentional omission: user-facing but too small | `nova/DOCS.md` | no | omit | Helpful docs support change, but too small and docs-only to deserve its own public release-note bullet. |
| Conversion-focused README rewrite with stronger product positioning and roadmap teaser | intentional omission: user-facing but too small | `README.md`, `CONTRIBUTING.md` | no | omit | This improves presentation and docs quality, but it does not add or change shipped product behavior enough to deserve a release-note bullet. |
| Cross-doc fixes for promoted skills, anchor targets, backup wording, and OpenCode URL | intentional omission: user-facing but too small | `README.md`, `PROJECT.md`, `nova/DOCS.md` | no | omit | Important doc accuracy cleanup, but still docs-only with no new user action or shipped feature delta. |
| Client-install docs contract update after README rewrite | internal-only omission | `tests/onboarding/client-install-docs-contract.test.ts` | no | omit | This protects docs correctness, but it is validation only. |
| Promoted live harness, contract tests, and proofing expansion | internal-only omission | `tests/**`, `scripts/e2e/**`, `package.json` | no | omit | Validation confidence only; no direct shipped behavior change by itself. |
| Dependabot prepare workflow hardening | internal-only omission | `.github/workflows/dependabot-safe-lane-prepare.yml`, `tests/onboarding/dependabot-automation-contract.test.ts` | no | omit | Release-process hardening, not end-user product behavior. |
| Reference docs and work specs for the promoted skills | internal-only omission | `docs/reference/**`, `docs/work/**` | no | omit | Supporting evidence and maintainer context only. |

## Exclusion Audit

Explicitly excluded from public release-note drafting unless they change shipped user behavior directly:

- `.github/workflows/dependabot-safe-lane-prepare.yml`
- `tests/**`
- `scripts/e2e/**`
- `docs/work/**`
- `docs/reference/**`
- `package.json` additions that only add maintainer/test scripts
- `README.md`, `CONTRIBUTING.md`, `PROJECT.md`, and `nova/DOCS.md` changes that are messaging, anchor, URL, or docs-accuracy follow-up only

Final exclusion statement:

- No remaining internal-only deltas are represented as standalone public release-note bullets.
- Post-`ea8a708` docs-only follow-up commits stay omitted, but the later merged `axios` security fix does add one selective `Bug Fixes` bullet.

## Evidence Notes

- Primary change source: PR `#162` (`feat: promote dashboard organize and history skills`)
- Supporting verification in PR `#162`:
  - targeted Vitest contract suite
  - `cd cli && go test ./...`
  - promoted smoke suite
  - promoted full suite
- Public-bullet evidence comes from shipped surfaces only:
  - `skills/dashboard/SKILL.md`
  - `skills/organize/SKILL.md`
  - `skills/history/SKILL.md`
  - `skills/ha-nova/SKILL.md`
  - `skills/fallback/SKILL.md`
  - `README.md`
  - `cli/client_gemini.go`
  - `scripts/onboarding/lib/install-local-skills-common.sh`

## Post-Feature Refresh Check

Commits added after the original `ea8a708` comparison:

- `0f635f7` on 2026-04-11: README and CONTRIBUTING rewrite plus docs-contract update
- `38b380b` on 2026-04-11: cross-doc fixes in `PROJECT.md` and `nova/DOCS.md`
- `b5e94aa` on 2026-04-11: README URL, anchor, and narrowed backup wording fix
- `1132ce7` on 2026-04-13: bundled root + `nova` `axios` bump to `1.15.0`

Refresh conclusion:

- The three April 11 commits improve docs quality and accuracy, but they do not introduce new shipped product behavior.
- The April 13 `axios` merge is different: it closes critical production dependency findings in the shipped app and Relay package.
- The release-note draft now gains one selective `Bug Fixes` bullet for that security update.

## Recommended Release-Note Draft

````md
## New Features

- Dashboard work now has a dedicated HA NOVA skill for safe storage dashboard edits, Lovelace resources, and targeted card changes.
- Organization tasks now have a dedicated skill for areas, floors, labels, categories, and richer entity and device metadata.
- History questions now have a dedicated read-only skill for bounded history, logbook timelines, and long-term statistics.
- Gemini now picks up newly shipped HA NOVA sub-skills automatically during install and sync, so the promoted skills arrive there without manual wiring.

## Bug Fixes

- Updated `axios` to `1.15.0` in both the main app and the Relay package to close critical production dependency findings.

## Install

**macOS / Linux**
```sh
curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/<RELEASE_TAG>/install.sh | HA_NOVA_VERSION=<RELEASE_TAG> bash
```

**Windows (PowerShell)**
```powershell
$env:HA_NOVA_VERSION = '<RELEASE_TAG>'
irm https://raw.githubusercontent.com/markusleben/ha-nova/<RELEASE_TAG>/install.ps1 | iex
```

## Already Installed?

Run `ha-nova check-update` or `ha-nova update`.
````

## Finalization Gate

Before turning this provisional draft into the final release body:

1. Re-resolve the exact publish SHA or release tag.
2. Re-run the same comparison if the target SHA changed after `1132ce7`.
3. Replace `<RELEASE_TAG>` only when the actual release tag exists.
