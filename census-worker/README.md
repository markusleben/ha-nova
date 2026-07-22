# HA NOVA Census Worker

The receiving end of the opt-in, ID-free census (see `PRIVACY.md` and
`docs/reference/census.md` in the repo root). One Cloudflare Worker plus one
SQLite Durable Object holding aggregate counter rows
`(iso_week, version, os, relay) -> count` — nothing else.

## Endpoints

- `POST /ping` — accepts exactly `{"schema":1,"version":...,"relay":...,"os":...}`
  (`relay` optional). Strict allowlist: unknown fields, bad shapes, bodies over
  512 bytes, or non-JSON content types are rejected. Success is `204` with no
  body.
- `GET /stats` — public accepted-ping aggregates (a sparse series within a
  26-week horizon,
  by_os/by_version/by_relay over the last 4 weeks, `peak_weekly_pings`),
  cacheable for an hour and CORS-open.
- Unsupported paths return `404`; unsupported methods on `/ping` return `405`.

## Design constraints

- No identifiers, no IP field or IP storage in the census application, no
  client timestamps. Worker observability and invocation logs are disabled in
  `wrangler.toml`.
- No client authentication. Any schema-valid unauthenticated payload can increment a
  counter, so the public data is directional and cannot prove unique installs.
- Counter cardinality is bounded to 256 rows per ISO week. New combinations
  beyond the cap fold into one `(other, other, other)` overflow row.
- The request logic is a pure core (`src/census.ts`) tested without the
  Cloudflare runtime in `tests/census-worker/worker.test.ts`, including a
  cross-contract check against the client payload (`cli/census.go`).
  A future upgrade path is `@cloudflare/vitest-pool-workers` for
  runtime-level tests of the Durable Object itself.

## Deploy (maintainer)

```sh
bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha>
```

The wrapper requires a clean checkout at that exact SHA, Node.js 22 or newer,
and `gh` authenticated to `github.com`. It proves the SHA is in the hard-pinned
`markusleben/ha-nova` main history, exact-pins Wrangler 4.113.0, runs a real POST + SQLite Durable Object
readback against an isolated local runtime, pins the Cloudflare account,
config, Worker name, and reviewed SHA tag, attests Wrangler's structured
deployment output, then verifies that exact Cloudflare version through a
cache-busted production `GET /stats`.

For the first stable launch only, `CENSUS_OBJECT_NAME` selects a fresh Durable
Object. The write smoke is local, so it cannot add a production count. Confirm
the public namespace is empty before publishing v0.21:

```sh
bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha> --require-empty
```

Do not change that name after launch without an explicit migration decision.

Rollback only to a reviewed worker commit that preserves the `public-v0.21`
namespace and the current `/stats` schema, then run the same wrapper with that
reviewed SHA and repeat the exact-version verification. The endpoint URL is
pinned in `cli/census.go`, `PRIVACY.md`,
`docs/reference/census.md`, and `README.md`.
