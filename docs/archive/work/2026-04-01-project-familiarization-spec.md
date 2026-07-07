Status: active
Date: 2026-04-01
Owner: Codex

# Project Familiarization Spec

## Goal

Build a precise working model of HA NOVA across:
- product/docs contract
- relay implementation
- Go CLI / installer lifecycle
- skill architecture
- tests, CI, and release gates

## Scope

In scope:
- read active SSOT docs
- inspect core source entrypoints and representative modules
- inspect representative skills
- inspect verification/release workflows
- run core verification where practical

Out of scope:
- behavior changes
- refactors
- release actions
- GitHub remote operations

## Deliverable

A concise internal summary of:
- system boundaries
- runtime flows
- safety model
- verification posture
- likely hotspots for future work
