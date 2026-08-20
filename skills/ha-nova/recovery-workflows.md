# Recovery Workflows

Shared contract for automations that monitor a health signal and act to
restore it (#568) and for workflows that repeat a recovery action after a
failed attempt (#569). Outcome evidence vocabulary and result classes:
`skills/ha-nova/outcome-verification.md` (#567).

## Liveness against persistent faults (#568)

Recognize watchdog intent: the user asks for a watchdog, self-healing,
continuous monitoring, or automatic recovery — behavior that must stay live
while the fault persists. An explicitly ONE-SHOT request ("restart it once
when it goes offline") is the one-shot class even when it uses recovery
vocabulary: continuity needs words that promise it ("keep it running",
"whenever it drops", "monitor it"), never the word "automatic" alone.

The defect: an edge trigger fires once per transition — a `numeric_state`
crossing, a `state` change to an unhealthy value, a binary health sensor
turning unhealthy, a template flipping false→true. A failed recovery with a
continuously unhealthy signal never re-fires the trigger, so one failed
attempt silently strands the fault. A Home Assistant restart while the
source is already unhealthy sees no transition either.

Distinguish four design classes before drafting or judging:

1. **One-shot transition action** — fires once per edge; promises no recovery.
2. **Recovery attempt with outcome verification** — acts once, re-checks health.
3. **Continuously live watchdog** — keeps re-evaluating while the fault persists.
4. **Failed recovery that escalates** — hands the persistent fault to a person.

Any watchdog-intent design requires ALL of:

- The monitored health signal is re-checked after the recovery action per
  `skills/ha-nova/outcome-verification.md` — still unhealthy is `failed`,
  never inferred success.
- An explicit persistent-fault path: bounded retry, periodic re-evaluation,
  or failure escalation.
- Startup coverage is REQUIRED for watchdog continuity: Home Assistant can
  boot into the fault, so the design carries a `homeassistant` start trigger
  or periodic re-evaluation that picks an already-unhealthy source up after
  restart. The startup path GATES on evidence: wait until the monitored
  signal is available (not `unknown`/`unavailable` from a still-loading
  integration) and CONFIRM the fault before acting — a bare start trigger
  that fires the recovery on every boot restarts healthy devices. Naming the
  gap is honest for a one-shot recovery, never a substitute for promised
  continuity.
- Multi-target recovery evaluates liveness independently per target — one
  recovered target never masks another's persistent fault.

Scope: ordinary one-shot threshold automations are never flagged when no
watchdog or self-healing intent exists. A design without a persistent-fault
path is honestly named a "one-shot recovery attempt", never a "watchdog".
Review reports the gap as a finding — it never rewrites the automation and
never executes recovery actions.

## Bounded retry policy (#569)

Retries are added only when the user explicitly requests recovery,
self-healing, or retry behavior — never onto ordinary service calls.

Evaluate eligibility BEFORE generating the workflow. Eight design questions
every retry design answers first:

1. Is the action safe to repeat?
2. Which semantic outcome proves success?
3. Which observed failure permits another attempt?
4. What is the maximum number of attempts?
5. What delay or backoff separates attempts?
6. How are overlapping recovery runs prevented?
7. What happens after attempts are exhausted?
8. Must retry/cooldown state survive a Home Assistant restart?

Rules:

- Physical-access, irreversible, non-idempotent, or otherwise unsafe actions
  are never auto-retried — offer escalation instead.
- An ambiguous transport failure never triggers a retry: the first action may
  already have been applied.
- Every retry keys on semantic failure evidence per
  `skills/ha-nova/outcome-verification.md` — a bare service-call error is
  never retry evidence. Each attempt gets its own bounded
  outcome-verification window. The probe must stay REACHABLE when the
  recovery action itself errors: Home Assistant aborts an action sequence on
  a raised error by default, so the recovery action carries
  `continue_on_error: true` (or an equivalent structure) so the semantic
  probe still runs; the raised error stays ineligible as retry evidence, and
  missing semantic evidence routes to the explicit non-retry failure path.
- The attempt count is finite and explicit; delay or backoff is explicit and
  chosen per case — no universal hard-coded values exist.
- Recovery exits immediately on verified success — and every retry attempt RE-CHECKS the health signal immediately before acting: a device that recovered during the delay/backoff makes the pending attempt moot, and acting on the stale failure would restart a healthy device.
- Exhaustion takes one explicit failure path and emits at most one
  notification per incident — never one per attempt.
- The automation `mode` and condition guards prevent overlapping recovery
  sequences; periodic re-entry uses a cooldown where necessary.
- Multi-target workflows isolate attempts, cooldown, and results per target.
- Persistent helpers are introduced only when state must survive a Home
  Assistant restart; either way, restart-survival behavior is stated in the
  preview.
- The preview states the maximum number of real actions that may occur.
