# Contributing

Thanks for being here. Let's build something great.

## 🧭 Project Principles

- MVP first — ship, learn, iterate.
- KISS — complexity is the enemy.
- Relay stays dumb. Skills stay smart.
- English-only for docs, code comments, and commits.

## 🚀 Quick Start

```bash
npm ci
npm run typecheck
npm test
```

That's it. If all three pass, you're ready.

## 🌿 Branch + Commit Style

- Conventional commit types: `feat`, `fix`, `refactor`, `build`, `ci`, `chore`, `docs`, `style`, `perf`, `test`
- Keep changes focused and reviewable.
- No repo-wide search/replace sweeps.

## 📬 Pull Requests

Before opening a PR:
1. `npm run typecheck` passes
2. `npm test` passes
3. Docs updated if behavior changed
4. Tests added for bug fixes when practical

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

This is the most important section. Read it before writing your first line of code.

**HA NOVA's core design: the LLM is the intelligence layer. The relay is infrastructure.**

Most HA integrations implement domain logic in server code — fuzzy entity search, config normalization, parameter handling, intent routing. HA NOVA deliberately avoids this. The LLM already has this knowledge. Skills refine and direct it. The relay just moves data.

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

- Review checks live in `skills/review/SKILL.md`.
- The meaning of codes like `R-10` or `H-09` is explained in `docs/reference/skill-architecture.md`.
- Keep those codes internal. User-facing output must use localized descriptive finding titles instead of exposing the codes directly.

## 🔒 Security

Do not open public issues for vulnerabilities.
Follow the reporting guidance in `SECURITY.md`.
