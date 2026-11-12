# Apply asks first on the App's safety check, then checks the plan again

Status: active (built for 0.31, HANO-58 lever 1)

Date: 2026-11-12. Owner: `ha-nova apply` (`cli/verb_apply.go`), the gate client (`cli/gate_client_plan.go`). High-risk: it reorders the confirmation flow of typed plans.

## Problem

A typed plan confirmed through the App's safety check (channel `relay`) checked the whole plan twice: once before the phone ask (`recheckBeforeAsk`) and once after the answer (`recheck` + `gateStillApproved`). On a large home over Home Assistant Link, the first check took 13–15 s (about 158 reads on the measured home), and the person waited that long for the phone to ring. Only the second check decides what is sent.

## Change

For a typed relay plan, `applyPlan`:

1. Runs the cheap checks on the stored plan before the ask:
   - the checks every apply already runs (claim, expiry and its upper bound, schema, server, seal, static refusals);
   - a typed code refused (`CONFIRM_IN_HOME_ASSISTANT`), as before;
   - the plan's fields still make its hash, and its id derives from that hash (`memberIntact`), so the phone never shows an edited plan.
2. Asks with the stored plan's first requests (`gateBeforeSend`; a batch asks for every stored member's write). Reads remembered during the ask are dropped.
3. After the answer, always runs the one full recheck. The existing drift handling applies: a new preview on drift, "already done" sends nothing, a refusal keeps its own code, a batch names the changed items. A drift after the person's OK starts its `say` with "Not done: your OK wasn't used, so nothing was sent." (`say.drift_after_ok`), and its next step tells the agent to ask once more and stop if it changes again. A failure that hands the plan back after the OK is `CONFIRMED_NOT_SENT`, as after a window or phone answer, so the agent never asks again on its own.
4. Sends only if `gateStillApproved` holds: the recheck still confirms the stored plan, the tier is unchanged, the requests are byte for byte the approved ones, and the plan is unexpired.

Every relay plan yields a request to ask for (rooms and dashboard plans never use the relay channel). If one ever yields none, apply still runs the recheck and then the earlier path: ask after the recheck, and check again after the answer. Plans that keep this computer's asks (window, phone link, form, chat code) are unchanged.

Invariant: nothing is sent unless the person approved AND the recheck after that approval passed on the exact same bytes. The typed tier never weakens.

## Accepted limits

- A drift since the preview shows after the answer, not before the ask. The phone then still shows "Approved: …", the result the worker writes at the tap; the computer says nothing was sent. The CLI has no route back to the phone after a tap, and none is added.
- A door already unlocked by the time of the answer costs one ask and then reports that nothing needed doing.
- A plan that drifted still costs one ask (the hourly limit is 10 per computer). So does a recheck that can't reach Home Assistant after the answer: nothing was sent, so the plan is handed back for a safe retry, and each retry asks again.
- In a chat app on a standalone container, a user whose allowed level shrank since planning still gets one phone ask; the access read after the OK refuses it.

## Tests

`cli/gate_ask_first_test.go`:
- the ask opens before any Home Assistant read of the apply, and the send comes only after the recheck read Home Assistant again;
- an edited call, batch member, config write, or raw plan is refused without asking;
- a drift after the OK says the OK wasn't used and sends nothing;
- a batch member that changed during the ask sends nothing (`BATCH_DRIFT`);
- a recheck that can't read Home Assistant after the answer sends nothing.

`cli/gate_client_review_test.go` `TestGateChecksThePlanAgainAfterTheApproval`: a door unlocked during the ask is now "already done" (exit 0, nothing sent), not `PLAN_DRIFT`; an expired plan is refused.
