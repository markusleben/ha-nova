# Claude Uninstall Project Memory Cleanup

## Problem

`ha-nova uninstall` can remove the Claude plugin, marketplace entry, cache, and install records, but Claude may still try to invoke HA NOVA skills afterward because HA NOVA-related project memory remains under `~/.claude/projects/*/memory/`.

Observed symptom:

- plugin state gone
- marketplace state gone
- cache gone
- Claude restart in the same project still tries `Skill(read)` / `Skill(ha-nova)`
- runtime responds `Unknown skill`

## Old Release Comparison

`origin/main` removed the Claude plugin and cache, but did not manage Claude project memory. The current ghost is therefore not a plugin-registry parity gap; it is a remaining Claude project-memory artifact that stayed outside uninstall scope.

## Goal

Make uninstall feel clean:

- after uninstall, Claude should not keep invoking removed HA NOVA skills because of HA NOVA-specific project memory
- unrelated Claude project memory must stay intact

## Non-Goals

- no broad deletion of `~/.claude/projects`
- no editing of unrelated user notes
- no attempt to fully manage generic Claude conversation history

## KISS Design

Add one small Claude project-memory cleanup step to uninstall:

1. Walk `~/.claude/projects/*/memory/`
2. Remove HA NOVA-specific sidecar memory files like `ha-nova-skills.md`
3. Never auto-delete Claude project-memory files
4. Keep mixed/unrelated `MEMORY.md` untouched
5. Print a clear final note when project memory may still mention HA NOVA

## Safety Rule

Project memory is Claude user data, not installer-owned state.

Therefore:

- do not auto-delete `ha-nova-skills.md`
- do not auto-delete `MEMORY.md`
- only detect and warn

## Tests

- uninstall leaves Claude project-memory files untouched
- uninstall emits a warning when HA NOVA-related Claude project-memory files are present
- uninstall output stays truthful about its actual removal scope
