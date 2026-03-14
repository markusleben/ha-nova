# HALMark Curated Adoption Design

**Date:** 2026-03-12
**Status:** Proposed
**Source reviewed:** `https://github.com/nathan-curtis/HALMark` (main branch, accessed 2026-03-12)

## Goal

Adopt a small, high-signal subset of HALMark ideas into HA NOVA without importing HALMark as a parallel framework, second source of truth, or relay concern.

## Context

HA NOVA already has strong safety primitives:
- no guessed IDs
- preview-before-write
- tokenized delete confirmation
- post-write review
- safe-refactoring guidance
- contract-heavy documentation and tests

HALMark adds value mainly as a stewardship-spec reference for Home Assistant AI behavior. Its strongest current asset is `spec/halmark.md`. Its benchmark harness is not adoption-ready yet: `arena/runner/halmark_runner.py`, `tests/arena_0/manifest.json`, and `tests/arena_0/cases/FG01_template_misuse.json` are empty in the reviewed public repository state.

## Constraints

- Keep Relay dumb.
- Keep intelligence in skills, review checks, and tests.
- Preserve UX-first behavior; avoid excessive warnings and lecture-like wording.
- Do not create a second normative spec copied from HALMark.
- Do not vendor or depend on the HALMark harness.

## Options Considered

### Option A: No formal adoption

Only acknowledge HALMark as inspiration and keep HA NOVA unchanged.

**Pros**
- zero maintenance cost
- no prompt bloat
- no attribution complexity beyond a thank-you

**Cons**
- misses several high-value safety improvements
- leaves useful wording and check patterns unharvested
- forum/community contribution does not visibly improve the product

### Option B: Curated subset adoption

Adopt only the HALMark ideas with strong HA NOVA fit as skill policy, review checks, and contract tests.

**Pros**
- best fit with HA NOVA architecture
- small maintenance surface
- immediate user-facing safety improvement
- easy to attribute cleanly

**Cons**
- requires selection discipline
- some HALMark ideas remain intentionally unused

### Option C: Broad HALMark integration

Import HALMark concepts widely, mirror its structure, or couple to its harness.

**Pros**
- strong external benchmark narrative
- wider conceptual coverage

**Cons**
- high maintenance burden
- second source of truth risk
- poor fit with current HALMark harness maturity
- easy to overfit file/diff-centric rules onto HA NOVA's API-first MVP

## Decision

Choose **Option B: Curated subset adoption**.

Adopt a narrow first package focused on behavior and safety rules that match HA NOVA's existing architecture and user experience. Keep all adoption in markdown skills, review catalog entries, and contract tests. Do not change relay runtime or create HALMark-derived runtime enforcement.

## Selected First-Pass Scope

### Primary adoption set

1. **FG-18: Silent Compliance with Invalid Premises**
- Add an explicit policy that HA NOVA must correct invalid Home Assistant premises instead of silently compensating for them.
- Keep the tone concise and non-preachy.

2. **FG-24: Targeting Ambiguity**
- Add an explicit rule for broad target ambiguity:
  area-wide targeting must trigger one clarification question when a narrower plausible scope exists.
- Apply mainly to service-call and write flows.
- Reuse the existing ambiguity budget: this must not create a second blocking ambiguity question in the same turn.

3. **FG-08: Scope Creep**
- Add an explicit minimal-diff / no-unrequested-rewrite rule for write and safe-refactoring flows.
- Frame as stewardship and reviewability, not rigid formatting purity.

4. **FG-15: Templated Event Names Are Silently Ignored**
- Add a precise review check for templated `event_type` misuse.
- Add one supporting guideline example.

5. **FG-17: Deletion Overshoot Without Verification**
- Add a delete/refactor verification rule focused on blast-radius confirmation after destructive changes.
- Express verification in HA NOVA terms, not Git-only terms.
- Note the tradeoff explicitly: HALMark currently marks this as `Candidate` and frames it in file/diff terms, but HA NOVA can still adopt the underlying stewardship behavior by requiring post-delete blast-radius verification through read-back and dependency checks instead of Git diff.

### Secondary / deferred set

These are useful, but not in the first adoption pass:

- **FG-09: Backup Warning Before Irreversible Changes**
  Defer as a soft policy. Good idea, but weaker while relay `/backups` is still unimplemented.
- **FG-13: Comment Safety and Visual Editor Trap**
  Defer as guide/doc wording. More important once file-based flows are stronger.
- **FG-10: YAML Structural Damage**
  Defer. Too file/diff-centric for the current API-first MVP.

## Concrete HA NOVA Mapping

### Policy-level changes

- `skills/ha-nova/SKILL.md`
  - add invalid-premise correction baseline
- `skills/write/SKILL.md`
  - add minimal-diff / no scope-creep rule
  - add delete verification wording
  - add targeting-ambiguity wording where write targets are broad
- `skills/service-call/SKILL.md`
  - add broad-target ambiguity rule
- `skills/guide/SKILL.md`
  - add invalid-premise correction wording for guidance flows
- `skills/ha-nova/safe-refactoring.md`
  - add scope-creep and destructive verify wording

### Review catalog changes

- `skills/review/checks.md`
  - add check for templated event names

### Contract coverage

- `tests/skills/ha-safety-contract.test.ts`
  - invalid-premise correction wording
- `tests/skills/ha-cross-skill-integration.test.ts`
  - scope-creep and targeting-ambiguity contract coverage
- `tests/skills/review-contract.test.ts`
  - review-catalog entry coverage for the new check
- new focused contract test if needed for delete verification wording

## What Will Not Be Adopted

- HALMark harness files
- HALMark scoring model
- broad import of Candidate footguns as hard policy
- direct relay/runtime changes
- copied HALMark spec mirror inside HA NOVA

## Attribution Strategy

Use lightweight attribution only where real influence exists:

1. Add a short acknowledgment in `README.md`.
2. Add targeted inline references near adapted checks/tests, for example:
   `Inspired by HALMark FG-15 by Nathan Curtis`
3. Optionally add a short `docs/reference/external-influences.md` page listing external inspirations.
4. Do not mirror HALMark prose wholesale.

This keeps HA NOVA as the only active source of truth while still giving visible credit.

## Success Criteria

This adoption pass is successful when:

- HA NOVA gains the five selected behaviors/checks without relay changes.
- New rules are enforced by contract tests, not only prose.
- The resulting skill wording stays compact and user-friendly.
- Attribution is visible but does not create normative duplication.

## Non-Goals

- full HALMark compatibility
- public benchmark claims based on HALMark
- importing HALMark governance, statuses, or scoring taxonomy
- adding low-signal template purity rules that users will not feel

## Recommendation

Proceed with a **small, clearly attributed rule-and-test subset**:
- `FG-18`
- `FG-24`
- `FG-08`
- `FG-15`
- `FG-17`

Treat `FG-09` and `FG-13` as follow-up candidates after the first pass lands cleanly.
