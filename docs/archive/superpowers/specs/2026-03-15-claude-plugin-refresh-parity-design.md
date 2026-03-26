# Claude Plugin Refresh Parity Design

## Problem

Claude on Windows still loaded stale HA NOVA skill content after setup.

Observed symptom:
- Claude executed the old relay path `~/.config/ha-nova/relay health`
- Current skill sources already say `ha-nova relay health`

This means the plugin payload inside Claude can remain stale even after HA NOVA setup/update rewires the marketplace source to a local payload.

## Goal

Make Claude registration deterministic and fresh:
- first install uses the local marketplace payload
- later setup/update refreshes an already-installed Claude plugin instead of leaving old cache content in place

## Decision

- Keep the local marketplace fix.
- Add explicit Claude plugin refresh semantics:
  - if `ha-nova@ha-nova` is already installed, run `claude plugin update ha-nova@ha-nova`
  - otherwise run `claude plugin install ha-nova@ha-nova`
- Apply the same rule in:
  - Go runtime installer/setup path
  - `scripts/onboarding/install-local-skills.sh`

## Why

- `marketplace add` alone does not prove that Claude refreshes an already-installed plugin payload.
- `install` on an already-installed plugin is not the right freshness contract.
- `update` is the explicit Claude-native refresh verb.

## Testing

- Go unit test: installed Claude plugin triggers `plugin update`, not `plugin install`
- Shell/dev contract: existing installed plugin triggers `plugin update`
- Full verify stays green
