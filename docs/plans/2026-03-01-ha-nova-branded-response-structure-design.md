# HA NOVA Branded Response Structure (Design)

Date: 2026-03-01  
Status: Proposed

## Goal

Define one consistent response contract for HA NOVA interactions (`read`, `write`, `debug`) that is:
- calm and low-anxiety for non-technical users
- fast in normal path
- strict on write safety

## Constraints

- Keep user-visible output compact.
- Keep terminology consistent: `App + Relay`.
- Ask questions only when blocking progress or preventing unsafe writes.
- Prefer deterministic defaults over extra back-and-forth.

## Brand Principles

1. Predictable shape every time.
2. First line = user reassurance + state of changes.
3. Plain language, low jargon, no blame wording.
4. One-step guidance, never a long checklist unless failure path.
5. Safety explicit for writes (`preview` -> `confirm` -> `apply`).

## Canonical Section Order

Use this exact order for all intents (omit empty sections):
1. `Outcome` — one-line status + confidence.
2. `Current State` — explicit mutation status (`No changes applied` / `Changes applied`).
3. `Impact` — only essential user-facing facts or diff.
4. `Gate` — next required decision (`Confirm`, `Need one choice`, `Blocked by X`).
5. `Next` — one immediate action with exact command/reply.

Compact trivial-read mode:
1. `Outcome`
2. `Current State`
3. `Next`

## Wording Style

- Voice: calm, direct, operational.
- Sentence length: short; mostly one clause.
- Avoid: apologetic filler, uncertainty dumping, internal tool narration.
- Use certainty tags:
  - `Confirmed:` verified by execution/result.
  - `Likely:` high-confidence inference.
  - `Unknown:` missing data; requires one question.

## Decision Gates

### Read Gate

- Default: execute directly, return result.
- Ask only if query target is ambiguous and auto-resolution is not deterministic.
- If multiple matches: show top options and ask one forced-choice question.

### Write Gate

- Always return preview before mutation.
- Require explicit confirm token (`confirm`).
- If user intent is broad/risky: narrow scope first (single object/entity).
- Post-write: return concise verification evidence + rollback path if available.

### Debug Gate

- Start with fastest failure classification:
  1. capability/connectivity
  2. auth/permission
  3. request shape/data
  4. platform behavior/regression
- Return minimal remediation for the detected class.
- Ask user only for one missing blocker input (token, entity id, expected behavior).

## Question Policy (When to Ask)

Ask exactly one question only if at least one is true:
- unsafe write risk without confirmation/scope
- multiple valid targets and no deterministic best pick
- missing required secret/permission/input
- conflicting user constraints

Do not ask when:
- safe default exists
- operation is read-only and can proceed
- additional question would not change the next action

Question format:
- one sentence max
- 2-3 options with recommended first
- include what happens next after answer

## Compact Templates

### Read Template

```md
HA NOVA | READ
Outcome: [Confirmed: <result summary>]
Current State: No changes applied.
Impact: [<k1>, <k2>, <k3>]
Gate: [None | Unknown: need one target]
Next: [optional one-step follow-up]
```

### Write Template (Pre-Confirm)

```md
HA NOVA | WRITE PREVIEW
Outcome: [Likely: ready to apply <change summary>]
Current State: No changes applied yet.
Impact: [Target: <id>] [Diff: <short diff>] [Risk: <low/med/high>]
Gate: Reply `confirm:<token>` to apply. [or choose A/B/C to scope]
Next: After confirm, I apply + verify in one pass.
```

### Write Template (Post-Apply)

```md
HA NOVA | WRITE RESULT
Outcome: [Confirmed: <change applied summary>]
Current State: Changes applied.
Impact: [Verification: <evidence>] [Changed objects: <count/list>]
Gate: [None | Blocked: <reason>]
Next: [optional rollback or follow-up command]
```

### Debug Template

```md
HA NOVA | DEBUG
Outcome: [Confirmed: failure class = <class>]
Current State: No changes applied. [or Changes applied: diagnostic-only]
Impact: [Error: <short>] [Scope: <component>] [Cause: <most likely>]
Gate: [Need one input: <exact input>] [or None]
Next: [single remediation step]
```

## Anxiety + Speed Safeguards

- First reassurance line always states whether anything changed.
- Never expose stack traces unless user asks.
- Max default response size: 5-8 lines for normal success paths.
- Failure paths: short diagnosis + one next action, not full decision tree.
- Maintain deterministic headings so users can quickly scan familiar structure.
