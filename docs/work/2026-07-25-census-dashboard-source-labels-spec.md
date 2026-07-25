# Census Dashboard Source Labels

Date: 2026-07-25
Status: active

## Goal

Make the private Census dashboard distinguish HA NOVA Census measurements from
the external Home Assistant Analytics Relay App count without requiring the
maintainer to infer the source from surrounding prose.

## Scope

- Add a prominent two-source explanation above the dashboard metrics.
- Label Census cards and the external Relay App card at the point of use.
- Show the Home Assistant Analytics dataset and Relay App slug in the external
  section.
- State that a HA NOVA Census reset does not reset the external metric and that
  the two sources must never be added.
- Keep the JSON schema, fetch behavior, counting logic, and Access policy
  unchanged.

## Acceptance

- Rendered HTML contains the source distinction, reset behavior, dataset URL,
  and Relay App slug.
- Existing Census and Home Assistant Analytics validation tests remain green.
- The dashboard remains usable on narrow and wide viewports.

## Non-goals

- No Census data reset in this change.
- No public dashboard.
- No new analytics collection.
- No release claim for an end-user feature.
