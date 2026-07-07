# Organize Label Readback Validator Fix

Date: 2026-04-05

## Problem

The promoted live validator for `organize_label_entity_flow` expects inline `config/entity_registry/get` markers in command text.
The live flow can still be correct when the readback uses file-based payloads named `*_entity_get*.json`.

## Decision

Accept either proof shape for the readback gate:
- inline `config/entity_registry/get`
- file-based `entity_get` or `entity-get` payload usage

## Scope

- `scripts/e2e/codex-ha-nova-promoted-live-e2e.py`
- one breadcrumb/choice note
