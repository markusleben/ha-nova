# Claude Windows BOM Registry Fix

Date: 2026-03-23

## Problem

Windows Claude setup could fail during install or rollback with:

`invalid character 'ï' looking for beginning of value`

The underlying issue was BOM-prefixed UTF-8 JSON in Claude's own registry files:

- `installed_plugins.json`
- `known_marketplaces.json`

HA NOVA treated those files as invalid JSON even though the payload itself was healthy.

## Decision

Trim a UTF-8 BOM before decoding Claude plugin and marketplace registry JSON.

## Scope

- `cli/client_claude.go`
- `cli/claude_marketplace.go`
- Claude regression tests

## Why

The Windows Claude path should not fail just because the registry file encoding includes a standard UTF-8 BOM.
