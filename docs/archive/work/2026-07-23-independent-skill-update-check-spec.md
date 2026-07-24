# Spec: Independent Skill Update Check

Status: merged — completed by #427
Date: 2026-07-23

## Problem

HA NOVA subskills are independently discoverable. A directly loaded subskill
currently runs only `ha-nova relay health`; the synchronous
`ha-nova check-update --quiet` contract lives only in the context skill.
The relay-health fallback compares against a local release cache before it
starts a detached refresh, so the first task can stay silent after a release.
It also cannot detect a compatible but stale Relay App above the minimum
version.

## Decision

- Add one shared session-bootstrap reference used by the context skill and
  every independently loadable subskill.
- Before the first HA task for each selected server profile in a session, run
  `ha-nova check-update --quiet` exactly once. SessionStart context and
  background JSON refreshes do not replace the human-output path because they
  cannot carry the pending census question.
- Suppress census handling only on additional same-session server-profile
  checks so profile switching cannot consume the three-callout cap.
- Keep the requested HA task primary. Surface notices after the task: HA NOVA
  updates and the census question at most once for the whole session; Relay
  updates at most once per selected server profile.
- Make the quiet update check include the existing exact Relay App update
  lookup, bounded by one four-second deadline across Relay health, states, and
  registry calls. The candidate must join `platform: hassio` with the exact
  NOVA Relay App `unique_id`; titles never establish provenance. A pending App
  update routes to `ha-nova:updates`; standalone Container/Core stays a manual
  image-pull path.
- Keep relay-traffic cache nudges as defense in depth, not the first-use
  correctness mechanism.
- Enforce the shared-bootstrap pointer across every subskill and preserve it
  through tree, flat, namespaced, and staged-plugin client installation.
- Bridge the v0.20+ copied Codex, OpenCode, current Antigravity, and Hermes
  layouts from the new binary's first validated Relay command. Retired
  pre-v0.20 Gemini/Antigravity roots remain explicit `ha-nova setup` migration
  territory instead of broadening a destructive hot-path scanner. The local repair runs before
  config/keyring access, persists a recovery plan before replacing files, and
  carries the update/census check after the already-loaded transition task's
  first successful `ws`/`core`/`files`/`backups` result without contaminating
  Relay JSON stdout. Health probes repair locally but never consume the
  one-shot carrier because hooks may suppress their stderr. A versioned marker
  makes later Relay calls O(1). Legacy Claude uses its existing SessionStart
  check; if its plugin cache is partial or stale, `ha-nova setup` remains the
  explicit recovery path.
- Transition limit: an already-loaded old prompt cannot learn the new
  per-profile session memory. Its one-time binary carrier covers the profile
  used by the first authenticated Relay task; switching to another profile in
  that same old session receives the full contract after starting the required
  fresh client session. Current skill copies check every selected profile
  immediately.

## Acceptance

- Every `skills/*/SKILL.md` entrypoint reaches the shared first-use contract
  before its first Home Assistant task and before any relay command.
- A direct-loaded subskill reports a newly published HA NOVA release on its
  first task even when the local release cache is stale.
- `check-update --quiet` reports a compatible above-floor Relay App update.
- The Relay lookup cannot multiply per-request timeouts; a blackholed Relay is
  abandoned when the one shared first-use deadline expires.
- Compatible current, missing, ambiguous, malformed, unreachable, and
  standalone Relay App evidence produces no update-available notice; a Relay
  below the compatibility floor still produces its existing warning and an
  evidence-based App-versus-container route.
- HA NOVA update and census notices remain post-task, once-per-session, and
  consent-gated; Relay notices are once per selected server profile.
- Existing JSON output and update-check exit codes remain unchanged.
- Old-copy repair is runtime-independent, refuses foreign regular directories
  and non-tree symlinks, survives partial replacement, preserves install state,
  and is idempotent across all four copied client layouts.
