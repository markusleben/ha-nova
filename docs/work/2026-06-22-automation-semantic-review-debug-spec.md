# Automation Semantic Review And Debug UX Spec

Status: active

Date: 2026-06-22

## Goal

Reduce avoidable Home Assistant automation write/debug failures without adding runtime smoke-test automation or Relay business logic.

## Changes

- Broaden R-18 so any same-block sibling variable dependency is flagged, regardless of key sort order.
- Add R-23 for boolean-like templates compared to string boolean literals such as `"True"` / `"False"`.
- Add R-24 as a low-severity advisory for capacity-like variables sourced from `available_energy`.
- Add `ha-nova trace latest <automation.entity_id|script.entity_id> [--json]` as a read-only Relay-backed helper.
- Make `snapshot save` explain missing input before JSON parsing.
- Clarify reload-timeout handling: retry reload once after write/readback; if it still times out, report saved config with runtime verification unknown.

## Non-Goals

- No automatic `automation.trigger` smoke tests.
- No generic Jinja renderer.
- No integration-specific SolarEdge rewrite rule.
- No Relay-side domain logic.
