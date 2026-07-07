# Dev-Sync Bundle Metadata Spec

## Problem

Local `npm run dev:sync` rebuilt the installed `ha-nova` runtime and copied the current `version.json`, skills, docs, and client registry. However, an older installed `bundle.json` could remain in place. `ha-nova status --json` then correctly reported the effective dev build as `0.7.0`, while the bundle metadata still showed the previous release version.

## MVP Fix

- Keep release bundle metadata unchanged.
- During dev-sync only, write a local `bundle.json` beside the rebuilt runtime.
- Use the repo skill version, current OS, current arch, and the expected public binary name.
- Do not add new runtime behavior or status heuristics.

## Verification

- `npm run dev:sync`
- `ha-nova status --json`
- Focused dev-sync tests
- Full `npm run verify`
