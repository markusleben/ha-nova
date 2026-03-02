# 2026-03-02 HA NOVA Fast-Pass Modular Design (KISS/MVP)

## Goal

Create a reusable, low-overhead skill architecture for Home Assistant write flows.
Primary targets: automation + script.

## Why

Observed issues in live sessions:
- user-facing output still includes outdated structure patterns
- too many exploratory steps before preview
- shell/runtime instability in complex command chains
- duplicated intent-resolution logic between flow types

## External Best-Practice Inputs

- LangGraph subgraphs: modular reusable workflow units with clear interfaces.
  Source: https://docs.langchain.com/oss/python/langgraph/use-subgraphs
- AWS Step Functions best practices: break monolith flows into composable modules.
  Source: https://docs.aws.amazon.com/step-functions/latest/dg/sfn-best-practices.html
- AWS modular workflow design pattern (parent/child workflows).
  Source: https://aws.amazon.com/blogs/compute/breaking-down-monolith-workflows-modularizing-aws-step-functions-workflows/
- Structured output contracts for deterministic downstream behavior.
  Source: https://openai.com/index/introducing-structured-outputs-in-the-api/

## MVP Architecture

Reusable blocks:
- B0_ENV
- B1_STATE_SNAPSHOT
- B2_ENTITY_RESOLVE
- B3_ID_RESOLVE
- B4_BP_GATE
- B5_BUILD_AUTOMATION
- B6_BUILD_SCRIPT
- B7_RENDER_DOMAIN_PREVIEW
- B8_CONFIRM_TOKEN
- B9_APPLY_WRITE
- B10_VERIFY_WRITE
- B11_DIAG_ONLY_ON_FAILURE

Fast-pass compositions:
- FP_AUTOMATION_CREATE_UPDATE = B0+B1+B2+B3+B4+B5+B7+B8+B9+B10
- FP_AUTOMATION_DELETE = B0+B3+B7+B8+B9+B10
- FP_SCRIPT_CREATE_UPDATE = B0+B1+B2+B3+B6+B7+B8+B9+B10
- FP_SCRIPT_DELETE = B0+B3+B7+B8+B9+B10

## Response Contract (Domain-First)

Normal success path must be domain-first and compact.
No internal orchestration details unless debugging/failure or user explicitly asks.

Automation preview/result fields:
- Automation Name
- Automation Goal
- Entities Used
- Behavior Summary
- Next Step

Script preview/result fields:
- Script Name
- Script Goal
- Inputs/Variables
- Actions/Entities Used
- Next Step

## Execution Constraints

- One full state snapshot per flow (reuse in all filters)
- No ad-hoc schema probing in normal path
- No temporary helper scripts in normal path
- For complex multiline commands, use `bash -lc`
- Avoid shell-specific builtins in portable paths (`mapfile`, zsh-specific constructs)

## Acceptance Criteria

1. Skills define reusable blocks and fast-pass compositions.
2. Domain-first response contract replaces legacy default structure wording.
3. Contract tests enforce block presence and domain-first response fields.
4. Contract tests reject legacy default structure wording in normal flow.
5. Verification passes (`skill-contract test` + `typecheck`).
