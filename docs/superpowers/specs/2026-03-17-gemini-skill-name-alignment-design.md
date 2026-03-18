# Gemini Skill Name Alignment

Date: 2026-03-17

## Problem

Gemini installs HA NOVA sub-skills into flat namespaced directories such as:

- `~/.gemini/skills/ha-nova-entity-discovery`

But the copied `SKILL.md` frontmatter still uses the short shared skill name:

- `name: entity-discovery`

This leaves Gemini with two different identifiers for the same sub-skill:

- folder/install name: `ha-nova-entity-discovery`
- activation name: `entity-discovery`

Observed result: Gemini can activate the skill successfully, but it may first guess the wrong longer name from the folder/resource path, then self-correct.

## Goal

Keep the Gemini flat hierarchy, but make Gemini's installed skill identifiers internally consistent:

- folder name
- frontmatter `name`
- copied parent-skill dispatch references

## Decision

Gemini-only flat copies will use namespaced sub-skill names:

- folder: `ha-nova-entity-discovery`
- frontmatter: `name: ha-nova-entity-discovery`

The Gemini-copied `ha-nova` context skill will rewrite its sub-skill references accordingly:

- `ha-nova:entity-discovery` -> `ha-nova:ha-nova-entity-discovery`

Shared repo source skill names stay unchanged for Claude, Codex, and OpenCode.

## Why

- preserves the required Gemini flat installation shape
- removes the name mismatch Gemini currently sees
- scopes the change to Gemini copies only
- avoids changing the shared repo skill contract for other clients

## Non-Goals

- no change to repo skill directory names
- no change to Claude/Codex/OpenCode naming
- no full redesign of skill dispatch wording
