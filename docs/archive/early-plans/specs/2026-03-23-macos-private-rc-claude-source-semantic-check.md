# 2026-03-23 macOS Private RC Claude Source Semantic Check

## Problem

The macOS Claude per-client helper validated `known_marketplaces.json` with a raw string grep.

Effect:
- equivalent Claude source serializations could fail the helper even when setup was correct
- helper behavior drifted away from the product code, which compares marketplace sources semantically

## Decision

Parse `known_marketplaces.json` and accept the current local Claude marketplace root when either:
- `source` is a matching string path
- `source.path` is a matching path
- `installLocation` is a matching path

Normalize candidate paths before comparison.

## Why

The private-RC helper should validate the same truth as the product, not a single incidental JSON serialization shape.
