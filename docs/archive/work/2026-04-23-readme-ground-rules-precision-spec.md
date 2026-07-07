# README Ground Rules Precision Spec

Date: 2026-04-23
Status: merged

## Goal

Make the README safety claims precise enough to match the shipped token model.

## Scope

- clarify that the Home Assistant token stays on the Home Assistant server
- clarify that the local Relay token copy uses the OS credential store
- scope the telemetry claim to HA NOVA
- keep the section short and user-facing
- add contract coverage for the precise wording

## Non-Goals

- no runtime behavior change
- no expanded security whitepaper
- no changes to setup flow

## Exit Criteria

- the README no longer implies every credential lives in the OS keychain
- the README no longer implies every tool in the stack has no telemetry
- the README still communicates the simple trust model quickly
