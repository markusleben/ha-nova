# Semantic Outcome Verification

Shared contract for runtime actions whose target state does not prove the
promised effect (#567). Owning skills reference this file from their verify
steps; direct state-changing actions keep their simpler existing path.

## Evidence classes

Classify BEFORE execution what the action's success can actually be proven by:

1. **Command acknowledgement only** — the response or a timestamp proves the
   command was registered, nothing more (a restart button's advanced
   timestamp, a notify service accepting a message).
2. **Direct target-state change** — the called entity's state carries the
   promise (a light's brightness). Existing per-skill verification applies.
3. **Indirect effect on related entities** — the promise lives on OTHER
   entities (a script's acted-on members, a scene's member states).
4. **Asynchronous completion signal** — the effect appears later on a
   defined signal (a refresh updating a sensor's `last_updated`, an update
   entity leaving `in_progress`). The signal must be attributable to THIS
   request: a `last_updated` that also advances on the integration's own
   polling cadence is ambiguous, and a value change alone attributes nothing
   either — routine polling changes values too. Attribution requires the
   advance to fall OUTSIDE the integration's known polling cadence (arriving
   within the observation window while the next scheduled poll is not yet
   due); with an unknown cadence, or an advance indistinguishable from
   routine polling, report `unverified`.
5. **No independently observable outcome** — nothing readable proves the
   effect (mobile notification delivery). Say so; never infer success.

## Probes

- Resolve effect probes from the action's own configuration, device or
  config-entry relationships (registry, `search/related`), or the user's
  explicit statement of intent. Friendly-name similarity alone NEVER selects
  a probe.
- The preview names the selected probes, the expected outcome per probe, the
  observation timeout, and any stability requirement.
- Capture every probe's baseline immediately before executing the action — after the confirmation, not at preview time: a baseline read while the confirmation waits goes stale, and an intervening actor's change would be attributed to this action. A probe whose expected value was ALREADY TRUE at that baseline proves nothing about this action (a skipped run leaves it unchanged too): report it as already-satisfied, and `verified` cannot rest on already-satisfied probes alone — with no probe left that could change, the honest overall outcome is `unverified`.
- Observation is bounded: a defined window (default: up to three reads over
  ten seconds; a probe with a known longer horizon states its own), never an
  indefinite wait.

## Results

Report one of exactly four outcomes, never a blend:

- `accepted` — command registered, promised effect not independently proven.
- `verified` — EVERY probe the previewed promise names showed its effect.
  One good probe never verifies a multi-probe promise (a scene, `scene.apply`,
  or a script acting on several entities): a partial result reports the
  per-probe split and the overall outcome stays `unverified` — or `failed`
  when any probe proved the opposite. Mutually exclusive `choose`/`if`
  branches are unioned for SAFETY classification only: verification binds to
  the branch that actually ran (trace or executed actions decide) — probes of
  untaken branches report "not applicable (branch not taken)", never
  `failed`. When NO source can identify the executed branch (no trace — a
  YAML item without an `id` — and effects that fit several branches), the
  branch is unknowable: report the per-branch readings honestly with overall
  `unverified`, never guess a branch and never mark any branch `failed`.
- `unverified` — evidence was expected but missing or unreadable at window
  end, or the transport outcome itself is ambiguous (a timeout before any
  acknowledgement: even registration is unknown — say that plainly, never
  retry). Missing evidence is never inferred success.
- `failed` — a probe showed the effect did NOT occur AFTER the observation
  window (including any stated transition time) elapsed — an opposite reading
  early in the window is in-progress, not failure — or the command itself was
  DEFINITIVELY rejected (an upstream 404/422 or an equivalent proven rejection — distinct
  from an ambiguous transport outcome, which never proves rejection and never
  permits a retry).

Verification NEVER repeats the original action automatically — not after
`unverified`, not after `failed`, and not after a transport error on a
disruptive or restart-class action. This binds the VERIFICATION step only: an
explicitly requested recovery workflow with a bounded, pre-approved retry
policy (`skills/ha-nova/recovery-workflows.md`) repeats by its own rules,
never by this step.

## Restart and reboot actions (#566)

A `button.press` / `input_button.press` whose semantics are restart, reboot,
or reset (entity naming plus device context plus the user's stated intent —
never friendly-name matching alone) is class 1 on its own timestamp: the
advanced timestamp is `accepted`, never restart proof. A recovery intent adds
a class-3/4 health probe on top:

- Initiated to recover a named unhealthy signal → re-check that signal
  inside a RESTART-LENGTH window (device-appropriate, stated in the preview —
  a rebooting device is unhealthy by design for a while, so the ten-second
  default is too short here): recovered → `verified`; still unhealthy after a
  window that covered the expected restart duration → `failed`; window too
  short or cut off → `unverified`, never `failed`.
- No independent effect signal → `accepted` plus the explicit sentence that
  the restart itself could not be independently verified.
- Ordinary buttons (no restart semantics) keep the timestamp as verification
  of the press itself.
