# Doc Review Orchestrator Port Spec

Date: 2026-04-12
Status: completed

## Goal

Make the local Claude agent `doc-review-orchestrator` usable from Codex by porting its review workflow into a local Codex skill.

## Source

- Claude agent: `/Users/markus/.claude/agents/doc-review-orchestrator.md`
- Claude memory: `/Users/markus/.claude/agent-memory/doc-review-orchestrator/MEMORY.md`

## Scope

- Create a local Codex skill under `~/.agents/skills/doc-review-orchestrator/`.
- Preserve the core review method:
  - accuracy
  - consistency
  - completeness
  - clarity
  - structure
- Adapt the workflow to Codex rules and tools instead of Claude-specific agent behavior.

## Porting Defaults

- Port as a skill, not as a Claude-style agent clone.
- Keep the skill general-purpose instead of copying project-specific memory into it.
- Preserve the five-pass review architecture.
- Allow parallel/subagent passes only when the user explicitly asks for subagents and the current tool context supports that.
- Keep the skill evidence-first and review-only by default; no rewrites unless the user asks.

## Deliverable

- `~/.agents/skills/doc-review-orchestrator/SKILL.md`
