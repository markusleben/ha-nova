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
- `GET /stats` — public aggregates (weekly series, by_os/by_version/by_relay
  over the last 4 weeks, `monthly_lower_bound`), cacheable for an hour,
  CORS-open. The same numbers for everyone, maintainer included.
- Anything else — `404`.

## Design constraints

- No identifiers, no IP storage, no client timestamps. Worker observability
  and invocation logs are disabled in `wrangler.toml`.
- The request logic is a pure core (`src/census.ts`) tested without the
  Cloudflare runtime in `tests/census-worker/worker.test.ts`, including a
  cross-contract check against the client payload (`cli/census.go`).
  A future upgrade path is `@cloudflare/vitest-pool-workers` for
  runtime-level tests of the Durable Object itself.

## Deploy (maintainer)

```sh
cd census-worker
npx wrangler deploy
```

After the first deploy, substitute the real `workers.dev` subdomain for the
`PLACEHOLDER` in `cli/census.go` (`censusEndpointURL`) and in the documented
stats URLs (`PRIVACY.md`, `docs/reference/census.md`, `README.md`) before the
release that ships the client feature.
