---
name: ha-nova
description: Use when the user wants Home Assistant operations through HA NOVA (App + Relay) with macOS Keychain-backed local auth.
---

# HA NOVA Skill

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

## Mission

Operate Home Assistant through HA NOVA with a simple user flow:
- App + Relay first
- fastest viable path first (avoid slow fallback paths unless required)
- minimal prompts
- safe write confirmation

## Runtime Prerequisite (macOS)

Before HA operations in this session:

1. Resolve repository root:
   - `NOVA_REPO_ROOT="${HA_NOVA_REPO_ROOT:-__HA_NOVA_REPO_ROOT__}"`
   - if missing script at `$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh`, fallback:
     - `NOVA_REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"`
   - if script is still missing, stop and ask user to:
     - clone `ha-nova`,
     - run `npm install`,
     - run `npm run install:skills`,
     - restart the client.
2. Verify session readiness once per active conversation:
   - Run exactly once at first HA operation:
     - `bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" ready --quiet`
   - After a successful result, do not run this check again in the same conversation by default.
3. Re-run readiness only on explicit capability failure:
   - examples: relay unreachable, auth rejected, missing required env/token for selected path
4. If readiness fails:
   - ask user to run `cd "$NOVA_REPO_ROOT" && npm run onboarding:macos`
   - then run `bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" doctor` for detailed diagnostics
   - stop until onboarding is healthy
5. Load runtime env from local config + Keychain (for this session):
   - `eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"`
6. Enforce mandatory LLAT:
   - if `HA_LLAT` is missing, stop immediately
   - route user to `npm run onboarding:macos` (setup) + `doctor`
   - continue only when LLAT validation is healthy

Do not ask user to paste tokens in chat.
Run preflight checks silently; only report them when they fail.

## Routing

- Setup/connectivity/user first-run questions:
  - use `"$NOVA_REPO_ROOT/skills/ha-onboarding.md"`
- Entity discovery/listing:
  - use `"$NOVA_REPO_ROOT/skills/ha-entities.md"`
- Device control:
  - use `"$NOVA_REPO_ROOT/skills/ha-control.md"`
- Automation CRUD:
  - use `"$NOVA_REPO_ROOT/skills/ha-automation-crud.md"` + `"$NOVA_REPO_ROOT/skills/ha-safety.md"`
- Automation enable/disable/toggle:
  - use `"$NOVA_REPO_ROOT/skills/ha-automation-control.md"` + `"$NOVA_REPO_ROOT/skills/ha-safety.md"`

## Safety Baseline

- Never guess entity IDs.
- Preview every write payload.
- Require explicit confirmation before write execution.
- Keep terminology as App + Relay.
