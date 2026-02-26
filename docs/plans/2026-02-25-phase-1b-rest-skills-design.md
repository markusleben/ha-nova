# Phase 1b REST Skills Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver the Phase 1b skill layer for direct REST-based entity discovery, device control, and automation lifecycle/control flows.

**Architecture:** Keep Bridge unchanged for Phase 1b. Add domain skills that route operations directly to Home Assistant REST API with LLT auth. Enforce core safety pattern (resolve -> preview -> confirm -> execute -> verify) for all writes.

**Tech Stack:** Markdown skills, Home Assistant REST API, existing `ha-nova` bootstrap routing.

---

## Scope

- Create `skills/ha-entities.md`
- Create `skills/ha-control.md`
- Create `skills/ha-automation-crud.md`
- Create `skills/ha-automation-control.md`
- Update `skills/ha-nova.md` only if routing/table gaps exist
- Create acceptance checklist `docs/phase-1b-acceptance.md`

## Out of Scope

- New Bridge endpoints
- Trace/diagnostics features (Phase 1c+)
- Full best-practice catalog integration (Phase 2)

## Acceptance Criteria

1. Four Phase 1b skill files exist and define concrete REST call flows.
2. Bootstrap routing references only existing skill files for Phase 1a/1b.
3. All write flows document preview + confirmation + verification.
4. LLT (`HA_TOKEN`) remains mandatory across workflow docs.
5. `docs/phase-1b-acceptance.md` records verification evidence.

## Execution Steps

1. Author `ha-entities` with discovery/search/get/service-list flows.
2. Author `ha-control` with service-call and post-call verification flow.
3. Author `ha-automation-crud` with list/get/create/update/delete/reload sequence.
4. Author `ha-automation-control` with enable/disable/toggle/trigger flow.
5. Run consistency checks between `ha-nova` catalog and actual files.
6. Record results in `docs/phase-1b-acceptance.md`.
