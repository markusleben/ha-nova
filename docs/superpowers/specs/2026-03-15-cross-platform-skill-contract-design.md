# Cross-Platform Skill Contract Design

## Problem

HA NOVA runtime onboarding is moving toward a real macOS + Windows product path, but the skill/documentation layer still contains shell-era assumptions.

The current drift shows up in three places:

1. **Claude plugin drift**
   - The Claude marketplace metadata still points plugin installation at the GitHub repo, not the tested local bundle/runtime.
   - Result: Claude users can end up with stale or repo-shaped skills instead of the exact tested release payload.

2. **Shell-specific skill instructions**
   - Many skills still document `ha-nova relay ws -d '...'`, backslash continuations, Unix pipes, `/tmp/...`, and shell redirection as the normal path.
   - Result: Windows PowerShell and mixed-shell clients are not reliably supported by the skill layer even when onboarding succeeds.

3. **Context/runtime mismatch**
   - The HA NOVA context skill still contains a bash-compatibility rule that conflicts with the current Windows product story.
   - Result: the runtime says Windows is supported, while the skills still imply “use bash-compatible shells.”

## Design Goal

Make onboarding and later skill usage follow one small cross-platform contract:

- **Runtime contract** stays `ha-nova ...`
- **Relay call contract** becomes shell-agnostic by default
- **Client-specific exceptions** stay explicit and documented only where needed
- **Claude plugin content** must come from the installed/tested payload, not from a drifting repo source

## Decision

### 1. One shell-agnostic relay command contract

For skill docs and agent instructions:

- Do **not** make inline `-d '...'` JSON the default cross-platform path
- Do **not** rely on Unix temp paths or shell redirection as the primary contract
- Prefer:
  - write JSON payload to a temp file with the client’s native file-writing tool
  - call `ha-nova relay ws --data-file <path>` or `ha-nova relay core --body-file <path>`
  - use `ha-nova relay ... --out <path>` for large outputs instead of shell `> file`

This keeps the relay contract compatible across:
- macOS shell clients
- Windows PowerShell clients
- mixed client runtimes that do not preserve bash quoting faithfully

### 2. Keep examples compact, but separate “command contract” from “shell snippet”

Skills should present:
- the **canonical relay contract** first
- optional bash examples only as examples, not as the only valid path

This preserves readability without making bash the product contract.

### 3. Claude plugin must resolve to the installed payload

Claude plugin installation/update must not silently depend on GitHub repo source.

Desired behavior:
- the installed bundle/runtime is the source of truth
- Claude setup/update must register or refresh the plugin from that installed payload
- failure to register/update must not be treated as a soft success on the supported Claude lane

### 4. Cross-platform support claims stay honest

After this change:
- onboarding, doctor, and resume must agree on readiness
- skills must use the shell-agnostic relay contract
- Windows Claude-specific prerequisites belong in `.claude/INSTALL.md`, not in the global README

## Scope

### In scope

- HA NOVA context skill runtime-shell contract
- All HA NOVA skill markdown that currently treats bash/Unix redirection as the default operational path
- Claude install/update path drift
- Client install docs for Claude/Codex/Gemini/OpenCode where platform-specific guidance is needed

### Out of scope

- Rewriting every example into two full shell variants
- Creating a new relay endpoint
- Changing HA NOVA runtime semantics beyond the already-planned readiness fixes
- Generalizing every third-party client quirk into the main README

## Desired End State

### Onboarding

- Windows and macOS onboarding both end at the same runtime contract: `ha-nova ...`
- `doctor` matches setup truth

### Skills

- First real skill call after onboarding works on both macOS and Windows without relying on bash-only quoting
- Large reads/writes use file-based relay flags by default

### Claude

- Installing/updating HA NOVA for Claude uses the installed payload, not a drifting repo source
- Windows Claude prerequisites are documented in `.claude/INSTALL.md`
- If Claude registration fails, HA NOVA does not overstate success

## Testing

- Contract tests for skill markdown migration to shell-agnostic relay patterns
- Claude install/update contract tests for installed-payload sourcing
- Real smoke:
  - Windows: onboarding -> doctor -> one real Claude skill call
  - macOS: onboarding -> doctor -> one real Claude/Codex/Gemini skill call
