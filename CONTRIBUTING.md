# Contributing

HA NOVA is still early. Small, focused improvements help most.

## Project Principles

- Relay stays dumb. Skills stay smart.
- MVP first. Ship, learn, iterate.
- Keep public docs thin. Update one active source of truth instead of restating it everywhere.
- Keep skills and skill-like source docs in English.
- Prefer small files. Split helpers before they turn into policy blobs.

## Setup

Prerequisites:
- Node.js `>=20`
- Go `>=1.24`

```bash
npm ci
npm run verify
```

`npm run verify` is the canonical host-safe gate. It covers production dependency audit, blocked-file checks, typecheck, docs contracts, the core safe Vitest slice, onboarding contracts, the host-safe build path, Go CLI tests, and release-contract verification. It must not open browsers or touch real secure stores on a maintainer machine. `npm run test:safe` remains the full Vitest sweep.

## Verification Matrix

Use the smallest verify slice that matches your change. If the change crosses boundaries, fall back to `npm run verify`.

| Change type | Minimum verification |
| --- | --- |
| Docs-only (`README.md`, `CONTRIBUTING.md`, client overlays, governance docs) | `npm run verify:docs` |
| Skill-only Markdown / prompt logic | `npm run test:safe` |
| Installer, onboarding, local dev helpers, client-install flow | `npm run verify:onboarding` |
| Relay runtime or Go CLI | `npm run verify` |
| Release metadata, release docs, workflow/release policy | `npm run verify:release-contracts` |

Canonical verify entrypoints:
- `npm run verify:docs`
- `npm run verify:installers`
- `npm run verify:onboarding`
- `npm run verify:release-contracts`
- `npm run verify`

Implementation detail: `npm run verify` uses `test:safe:core` between the docs and onboarding slices so those owned contract tests do not rerun twice. `test:safe:core` is a closed allowlist in `scripts/test/safe-core-files.json`, and `npm run verify:onboarding` starts with `npm run verify:installers` so the committed public installers cannot drift away from `scripts/install-src/`.

## Architecture Boundaries

Read these before changing runtime behavior:

- `docs/reference/bridge-architecture.md`
- `docs/reference/skill-architecture.md`
- `docs/reference/documentation-governance.md`

Boundary test:
1. Could an LLM do this from raw data alone? If yes, it belongs in a skill.
2. Does it need host access, persistent connections, or token storage? If yes, it may belong in the relay or CLI.
3. Does it interpret, rank, or decide? Keep that in skills.
4. Does it transport, store, or expose data? Keep that in the relay or CLI.

Do not add business logic to relay handlers. New relay endpoints need a clear infrastructure justification.

## Review Check Taxonomy

Contributor entrypoints for the review system:

- `docs/reference/skill-architecture.md`
- `skills/review/SKILL.md`
- `skills/review/checks.md`

Keep review codes internal. User-facing review output must use descriptive localized finding titles instead of raw codes.

## Writing Skills

Skills are plain Markdown under `skills/`.

When adding or updating a skill:
1. Follow the required section template in `docs/reference/skill-architecture.md`.
2. Reuse existing patterns before inventing a new flow.
3. Keep skill files English-only.
4. Update the dispatcher / skill tree when a new skill is added.

## Pull Requests

Before opening a PR:
1. Run the minimum verification for the touched area.
2. Update the active source-of-truth doc if behavior changed.
3. Add or update regression coverage when it fits.

Every PR should explain:
- Problem
- Solution
- Risk
- Verification

Keep PRs focused. Avoid repo-wide search/replace sweeps.

## Advanced / Internal Paths

These are useful, but they are not the default contributor path:

- `npm run dev:sync`
- `npm run dev:install:codex-skill`
- `npm run dev:install:claude-skill`
- `npm run dev:install:opencode-skill`
- `npm run dev:install:gemini-skill`
- `npm run dev:install:skills`
- `npm run test:desktop:macos`
- `npm run test:desktop:windows:headless`
- `npm run test:desktop:windows:rdp`
- `npm run dev:validation:harness`

Use `npm run dev:install:*` when you need a fresh repo-local skill install for one client. Use `npm run dev:sync` only when that repo-dev install already exists and you just need the current checkout pushed into the local client state.

`scripts/onboarding/install-local-skills.sh` and `scripts/onboarding/bin/ha-nova` are repo-dev helpers, not supported end-user product interfaces.

Desktop validation and private RC flows stay in `docs/releasing.md`. Keep maintainer runbook detail there instead of copying it here.

Emergency cleanup if a local desktop-validation helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\.sh|http\.server 8917|vitest|mock-ha-relay\.py|ha-nova setup' || true
```

## Security

Do not open public issues for vulnerabilities. Follow `SECURITY.md`.
