# Claude Local Marketplace Source Design

Date: 2026-03-15

## Problem

Real macOS setup fails in Claude Step 4.

Observed error:

`Failed to parse marketplace file ... plugins.0.source: Invalid input`

Current Go/runtime rewrite replaces the marketplace plugin `source` with an absolute filesystem path. Claude accepts relative string sources like `./` or `./plugins/ha-nova`, but rejects absolute path strings in marketplace plugin entries.

## Goal

Keep production/update behavior intact while making local/install-time Claude marketplace registration valid on macOS and Windows.

## Constraints

- Keep repo template marketplace pointing at Git/GitHub for release/update semantics.
- Installed payload may rewrite its own local marketplace manifest.
- Dev/local skill installer may stage a separate local marketplace root.
- KISS; no new distribution system.

## Design

### Installed bundle

- Marketplace root: install root
- Marketplace plugin `source`: `./`
- Rewrite only the installed copy of `.claude-plugin/marketplace.json`

### Dev/repo local install

- Marketplace root: `~/.config/ha-nova/claude-marketplace`
- Stage `ha-nova` under that root as symlink/copy to repo root
- Marketplace plugin `source`: `./ha-nova`

## Verification

- Unit tests assert rewritten manifest uses relative local source and no GitHub URL.
- `claude plugin marketplace add <root>` succeeds against rewritten/staged manifests.
- Focused onboarding/client tests stay green.
