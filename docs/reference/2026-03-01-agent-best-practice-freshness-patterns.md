# Agent Best-Practice Freshness Patterns (2026-03-01)

## Scope

Compare how established agent ecosystems keep behavior aligned with changing guidance, then derive a pattern for HA NOVA automation writes.

## Observed Patterns

1. Layered instruction model (global + repo + task-level)
   - Common in GitHub Copilot and Cursor rules.
2. Session memory/checkpoint model
   - Common in OpenAI Agents sessions and LangGraph short-term memory.
3. Hooked refresh points
   - Common in Claude Code hooks (`SessionStart`, pre/post tool).
4. Risk-gated enforcement
   - Stronger checks for write/destructive operations than read-only flows.
5. Source provenance
   - Keep timestamp and source list for decisions that depend on current docs.

## Applied Decision for HA NOVA

- Keep static base rules in skills.
- Add mandatory per-session refresh gate for automation `create`/`update`.
- Restrict authoritative refresh sources to official Home Assistant docs + release notes.
- Block writes if refresh fails.

## Sources

- OpenAI Agents SDK guardrails:
  - https://openai.github.io/openai-agents-js/guides/guardrails/
- OpenAI Agents SDK sessions:
  - https://openai.github.io/openai-agents-js/guides/sessions/
- OpenAI session memory cookbook:
  - https://cookbook.openai.com/examples/agents_sdk/session_memory
- Claude Code best practices:
  - https://code.claude.com/docs/en/best-practices
- Claude Code hooks guide:
  - https://code.claude.com/docs/en/hooks-guide
- GitHub Copilot repository custom instructions:
  - https://docs.github.com/en/copilot/customizing-copilot/adding-repository-custom-instructions-for-github-copilot
- GitHub Copilot CLI repository instructions:
  - https://docs.github.com/en/copilot/how-tos/copilot-cli/add-repository-instructions
- Cursor rules:
  - https://cursor.com/docs/context/rules
- LangGraph memory:
  - https://langchain-ai.github.io/langgraph/how-tos/memory/
- Home Assistant automation YAML:
  - https://www.home-assistant.io/docs/automation/yaml/
- Home Assistant automation modes:
  - https://www.home-assistant.io/docs/automation/modes/
- Home Assistant automation trigger:
  - https://www.home-assistant.io/docs/automation/trigger/
- Home Assistant automation troubleshooting:
  - https://www.home-assistant.io/docs/automation/troubleshooting/
