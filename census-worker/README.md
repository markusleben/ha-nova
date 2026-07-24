# HA NOVA Census Worker

Cloudflare Worker and SQLite Durable Object for the explicitly opt-in HA NOVA
Census. See `PRIVACY.md` and `docs/reference/census.md`.

## Routes

- `POST /ping`
  - schema 2: strict
    `schema, installation_id, version, relay?, os` allowlist
  - schema 1: accepted into legacy activity counters only
  - maximum 512-byte JSON body
- `POST /withdraw`
  - strict `schema, installation_id` allowlist
  - deletes the matching hashed installation record
- `GET /stats`
  - private maintainer HTML dashboard
- `GET /stats/api`
  - private machine-readable aggregate statistics

Cloudflare Access must protect `/stats*`. The Worker additionally validates the
`Cf-Access-Jwt-Assertion` against `ACCESS_TEAM_DOMAIN` and `ACCESS_AUD`.
Production responses are `private, no-store`.

## Storage and counting

The Worker SHA-256 hashes the random Census ID before the Durable Object sees
it. The table stores the hash, current dimensions, and Worker timestamps.
Repeat reports from one ID update one row.

- active: last report within 21 days
- known: last report within 60 days
- automatic deletion: 60 days after the last report
- no per-ID stats route
- maximum 20,000 retained installation rows
- maximum 500 previously unseen installation IDs admitted per UTC day
- version and Relay breakdowns show the 20 largest rows plus `other`

Relay absence is counted as “not recently observed,” not as a version.
Identifier-free schema-1 pings remain separate and retain the existing
256-combinations-per-week abuse bound.

The public endpoint cannot distinguish an official client from a fabricated
valid payload. Per-ID mutation limits, daily admission, retained-row, and
breakdown-cardinality bounds limit resource consumption; they do not make the
count verified. The private dashboard exposes same-day admission rejections so
the maintainer can detect pressure.

The dashboard fetches the official Home Assistant Analytics add-on entry
`2368fcfa_ha_nova_relay`. Relay App installations and HA NOVA client
installations are separate metrics and must never be added.

## Privacy boundary

Cloudflare processes the HTTPS source IP and connection metadata. Worker
observability and invocation logs are disabled. HA NOVA request code does not
read source-IP headers, `request.cf`, or geography. An AST contract test
allowlists every read from the incoming `Request`.

## Deployment

Set the existing Worker secrets before deployment:

- `ACCESS_TEAM_DOMAIN` — for example `https://example.cloudflareaccess.com`
- `ACCESS_AUD` — Access application audience

The release verifier uses a Cloudflare Access service-token policy and expects
these local environment variables:

- `HA_NOVA_CENSUS_ACCESS_CLIENT_ID`
- `HA_NOVA_CENSUS_ACCESS_CLIENT_SECRET`
- `HA_NOVA_CENSUS_BROWSER_ACCESS_VERIFIED=1` — set only after a fresh
  maintainer browser login reaches `/stats`

Deploy only a fully reviewed merge:

```sh
bash scripts/release/deploy-census-worker.sh <reviewed-merge-sha>
```

The wrapper requires Cloudflare Access to protect `/stats` before deployment,
proves both unauthenticated denial and authenticated service-token access, and
requires the explicit fresh browser-login attestation above. It then proves
locally:

1. two schema-2 reports with one ID produce one installation;
2. schema-1 remains a separate legacy activity count;
3. missing and incorrect local stats credentials are rejected;
4. withdrawal removes the schema-2 installation.

After deployment it repeats the same proof with a random ephemeral production
ID, verifies the exact Cloudflare version metadata, and restores the pre-smoke
count through `/withdraw`. Any post-deploy verification failure automatically
rolls the Worker back to the previously active 100-percent version.

Do not rename `CENSUS_OBJECT_NAME` without an explicit migration decision.
