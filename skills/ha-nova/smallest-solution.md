# Smallest Complete Solution

The default for EVERY solution this skill set designs — automations, scripts,
helpers, scenes, dashboards, YAML configuration. Applied silently: never
mention KISS, MVP, or this contract unless the user asks about the approach.

## Selection rules (at design time, before any preview)

1. Fulfil the COMPLETE explicit user outcome with the simplest safe and
   correct design. Simple never means partial.
2. Add nothing for hypothetical future needs: no helper, watchdog, fallback,
   option, mode, or abstraction that the request does not require. One
   automation that does the job beats an automation + helper + guard chain
   that might generalize later.
3. Never treat as optional complexity: required dependencies, safety checks,
   evidence gathering, compatibility handling, confirmation, verification,
   and recovery paths. They are part of "complete", not extras to trim.
4. Explicitly requested advanced capabilities are fully represented — honour
   every stated requirement, then add no structure beyond them.
5. Prefer native Home Assistant primitives when they are equally safe and
   clearer (a trigger `for:` over a timer helper; a scene over four service
   calls; a blueprint the user already uses over a parallel automation).
6. Reuse existing configuration only when it is GENUINELY simpler and does
   not couple unrelated behaviour. Never overload an existing helper,
   automation, or script merely to keep the object count down — two small
   independent items beat one entangled one.

## Unsolicited improvements

- Offer at most TWO per logical task, each directly relevant to the stated
  goal and backed by observed evidence (entity capabilities, current config,
  history) — never generic best-practice padding.
- Present the smallest intervention first.
- When the requested solution is already complete, offer nothing — do not
  invent suggestions to fill the block.
- Accepted suggestions join the pending draft BEFORE its single preview and
  write cycle (no second write pass).
- Not "unsolicited": anything the user asked for — reviews, brainstorming,
  findings, safety actions, recovery, verification, and test offers keep
  their own flows and caps.
