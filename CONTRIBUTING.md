# Contributing

HA NOVA is early. If you want to help shape it, this is the place.

## 🧭 Project Principles

- MVP first — ship, learn, iterate.
- Keep it simple. Complexity is the enemy.
- Relay stays dumb. Skills stay smart.
- All docs, code comments, and commits in English.

## 🚀 Quick Start

Prerequisites:
- Node.js `>=20`
- Go `>=1.24`

```bash
npm ci
npm run verify
```

`npm run verify` is host-safe by design. It runs release metadata checks, production `npm audit` on both the root and `nova/` lockfiles, TypeScript checks, the safe Vitest suite, and Go CLI tests.
It must not open browsers or touch real secure stores on a maintainer machine.

Explicit desktop validation stays separate:

```bash
npm run test:desktop:macos
```

That command rebuilds fresh private RC bundles locally and serves them to the macOS helper lane automatically.

Windows validation stays script-first in the VM:
- headless: `npm run test:desktop:windows:headless`
- desktop/RDP: `npm run test:desktop:windows:rdp`
- set `HA_NOVA_BUNDLE_URL` and `HA_NOVA_BUNDLE_SHA256_URL` first
- optional for RDP: set `HA_NOVA_CLIENT=claude|codex|opencode|gemini`
- for release-bound Windows work, use `scripts/dev/windows-clean-test-state.ps1` first and follow the private RC checklist in `docs/releasing.md`

Repo-dev refresh stays separate from product installs:
- `npm run dev:sync`
- `npm run dev:install:codex-skill`
- `npm run dev:install:claude-skill`
- `npm run dev:install:opencode-skill`
- `npm run dev:install:gemini-skill`
- `npm run dev:install:skills`

Preferred contributor flow:
- use `npm run dev:install:*` when you need a fresh local skill install for one client
- use `npm run dev:sync` when you already have a repo-local install and just need the latest repo state pushed into those local client caches/wrappers

Emergency macOS cleanup if a desktop helper was interrupted:

```bash
pkill -f 'npm run dev:validation:harness|start-local-validation-harness\\.sh|http\\.server 8917|vitest|mock-ha-relay\\.py|ha-nova setup' || true
```

If you are only touching the Go runtime, the minimum fast path is:

```bash
npm run test:cli
```

## 🌿 Branch + Commit Style

- Conventional commit types: `feat`, `fix`, `refactor`, `build`, `ci`, `chore`, `docs`, `style`, `perf`, `test`
- Keep changes focused and reviewable.
- No repo-wide search/replace sweeps.

## 📬 Pull Requests

Before opening a PR:
1. `npm run verify` passes
2. Docs updated if behavior changed
3. Tests added for bug fixes where possible

Every PR should explain:
- **Problem** — what's wrong or missing
- **Solution** — what you did and why
- **Risk** — what could break
- **Verification** — how to confirm it works

### What happens after you open a PR

1. **CI runs automatically** — typecheck, tests, build, docs fact-check, CodeQL analysis.
2. **Codex review bot** may post inline code review comments (optional, non-blocking).
3. **Maintainer review** — all PRs require an approving review before merge.
4. **Squash merge** — PRs are squash-merged to keep the history clean.

## 🧠 Architecture Philosophy

This is the most important section. Read this before writing any code.

**HA NOVA's core design: the LLM is the intelligence layer. The relay is infrastructure.**

Most HA integrations put domain logic in server code — fuzzy entity search, config normalization, parameter handling, intent routing. HA NOVA deliberately avoids this. The LLM already knows this stuff. Skills refine and direct it. The relay just moves data.

Repo shape:
- `nova/` = Home Assistant App / Relay runtime
- `cli/` = Go-first local runtime (`setup`, `doctor`, `update`, `uninstall`, `relay`)
- `skills/` = markdown skills
- `scripts/` = bootstrap, release, smoke, and dev-only support helpers

### 🧪 The Boundary Test

Before adding code to the relay, run these four questions:

1. **Could an LLM do this given the raw data?** → Skill
2. **Does it need platform access the LLM doesn't have?** (filesystem, persistent connections, network) → Relay
3. **Does it interpret, rank, or decide?** → Skill
4. **Does it transport, store, or provide access?** → Relay

> **The litmus test:** if you removed the relay endpoint and gave the LLM the raw data instead, would the feature still work (maybe slower)? If yes — the logic belongs in a skill.

### 📋 Concrete Examples

| Feature | Where | Why |
|---|---|---|
| Fuzzy entity search | 📝 **Skill** | LLMs handle fuzzy matching natively — typos, abbreviations, multilingual input. No matching algorithm needed. |
| Config normalization | 📝 **Skill** | The skill teaches the AI the correct YAML format. HA validates on write. |
| Domain knowledge (*"lights have brightness"*) | 📝 **Skill** | LLMs know this. The skill reinforces HA-specific details. |
| Detect conflicting triggers | 📝 **Skill** | Requires reasoning about trigger semantics — pure intelligence. |
| Suggest energy-saving automations | 📝 **Skill** | Pure reasoning over existing config data. |
| WebSocket message forwarding | 🔧 **Relay** | Needs persistent WebSocket connection on the host. |
| REST request forwarding | 🔧 **Relay** | Needs network access to the HA API on the host. |
| Token storage on HA host | 🔧 **Relay** | Needs filesystem access — keeps secrets off the client. |

### 📐 What "Infrastructure" Means

The relay can filter, paginate, and cache data — like a database index. It must not score, rank, validate, or make decisions about that data.

A `domain=light` filter is infrastructure (a WHERE clause). A fuzzy scorer that ranks results is business logic. One proxies, the other decides.

### ✅ Guardrails

- No business logic in relay handlers.
- Keep skills as plain Markdown (`*.md`).
- Prefer small files — split when complexity grows.
- New relay endpoints need a clear **infrastructure justification** in the PR description.

## 🏷️ Review Check Taxonomy

- Review entrypoint lives in `skills/review/SKILL.md`.
- The full check catalog lives in `skills/review/checks.md`.
- The meaning of codes like `R-10` or `H-09` is explained in `docs/reference/skill-architecture.md`.
- Keep those codes internal. User-facing output must use localized descriptive finding titles instead of exposing the codes directly.

## 📝 Writing Skills

Skills are plain Markdown files under `skills/`. To add or modify a skill:

1. Follow the **Skill Section Template** in `docs/reference/skill-architecture.md` (required sections: Scope, Bootstrap, Flow, Output Format, Safety, Guardrails).
2. Study existing skills as examples — start with `skills/service-call/SKILL.md` for a straightforward inline skill.
3. Add dispatch entries and update the skill tree per the **Adding a New Skill** checklist in `docs/reference/skill-architecture.md`.

All skill files must be 100% English. See `docs/reference/skill-architecture.md` for the full architecture and conventions.

## 📚 Documentation Rules

Use the active documentation map in `docs/reference/documentation-governance.md`.

Working defaults:
- public product/install/support truth belongs in `README.md`
- contributor workflow belongs in `CONTRIBUTING.md`
- release/runbook truth belongs in `docs/releasing.md`
- relay/API/reference truth belongs in `docs/reference/`
- Home Assistant App / relay operator truth belongs in `nova/DOCS.md`
- active skill behavior belongs in `skills/**/SKILL.md`

Do not create new active docs under `docs/archive/superpowers/`.
That archive path is historical work-history, not the current place for active product truth.

If you need a temporary working doc:
- create it under `docs/work/`
- keep it short
- use one file per topic
- update the real SSOT in the same PR that lands the behavior
- archive or delete the temporary work doc immediately after

## 🔒 Security

Do not open public issues for vulnerabilities.
Follow the reporting guidance in `SECURITY.md`.

## 🛠️ Dev Helpers

The only remaining `scripts/onboarding/` files are repo-dev helpers:
- `scripts/onboarding/install-local-skills.sh`
- `scripts/onboarding/bin/ha-nova`

They are not part of the supported end-user product contract.
They are also not part of the default host-safe verification gate.
