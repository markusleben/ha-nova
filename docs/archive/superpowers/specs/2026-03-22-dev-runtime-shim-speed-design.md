# Dev Runtime Shim Speed Design

## Goal

Make repo-local shell shims feel fast during development without changing the installed product runtime.

## Problem

The legacy shell wrappers fall back to `go run . ...` when no installed `ha-nova` binary exists.
That makes every local shim invocation pay Go toolchain startup + build/link validation cost again.

Measured locally on Markus's machine:

- warm `go run . version`: about `0.12s` to `0.17s`
- cold `go run . version` with empty cache: about `8.9s`
- built binary `./ha-nova version`: effectively instant

## Decision

Keep installed runtime lookup first.
When wrappers must use the repo checkout, replace direct `go run` fallback with a cached `go build -o <user-cache>/ha-nova` dev binary and execute that binary.

## Scope

- `scripts/onboarding/bin/ha-nova`
- `scripts/update.sh`
- `scripts/version-check.sh`
- `scripts/onboarding/uninstall.sh`
- shared helper under `scripts/lib/`

## Non-Goals

- no end-user installer/runtime behavior change
- no Go CLI package refactor just for startup speed
- no new daemon/watcher

## Acceptance

- installed runtime still wins when present
- repo fallback rebuilds only when `cli/*.go`, `cli/go.mod`, or `cli/go.sum` changed
- repeated wrapper invocations reuse the built dev binary
- contract tests reflect the new delegation path

## Cache Validity

The cached dev binary must also rebuild when the Go build context changes, even if source mtimes did not.

Tracked build-context stamp:

- `GOVERSION`
- `GOOS`
- `GOARCH`
- `CGO_ENABLED`
- `GOFLAGS`
- `GOEXPERIMENT`
