# 2026-03-23 Doc Obsolete Audit

## Goal

- Minimize active doc drift and remove clearly dead planning artifacts after the Windows lifecycle refactor.

## Decisions

- Treat `published and proven` as the current Windows public-doc gate; publication alone is not enough.
- Delete plan files that only describe the abandoned `ha-nova:guide` skill and have no remaining active consumers.
- Keep older plans/specs that still explain shipped work, but stamp them as historical when their body references superseded shell-onboarding or pre-fallback shapes.
- Rewrite stale internal docs when they still teach the wrong current runtime path, instead of preserving them as-is.

## Non-Goals

- No archival-folder reshuffle in this pass.
- No attempt to rewrite every historical plan body to modern terminology line-by-line.
