# Update-Notice Release Highlights Spec (#403)

Status: merged — #408
Date: 2026-07-21
Trigger: issue #403 — update notices show only versions; users cannot tell why an
update matters or whether it needs action. The GitHub release body already says,
but the decoder/cache discard it. Three presentation paths can drift.

## Design (issue is authoritative on caps/behavior; explorer-verified surface)

- **Decode more:** `githubRelease` (`cli/release.go:13-20`) gains `body`,
  `published_at`. On a 200, derive a deterministic digest; cache only the
  normalized highlights, never the full body.
- **New module `cli/release_digest.go`** (<400 LOC, own file):
  - Recognize top-level bullets under: action-needed sections (`Breaking
    Changes`, `What To Watch`, `Upgrade Notes`), `New Features`, `Bug Fixes`.
  - Keep max 1 action-needed + 2 feature/fix items; bullet order = importance.
  - Strip code blocks, install commands, nested details, control characters,
    presentation-only Markdown; caps 220 chars/item, 700 total.
  - One shared formatter used by `check-update` human output
    (`humanNoticeFromUpdateCheckResult`), the relay nudge
    (`skillUpdateNudgeMessage`), and the session hook.
- **Cache schema (additive, `omitempty`):** `releaseInfo` and
  `updateCheckResult` gain `published_at` + `release_highlights: [{kind, text}]`.
  Old caches parse fine both directions.
- **304 starvation fix (acceptance criterion):** when the cached entry lacks
  digest metadata, skip `If-None-Match` so one 200 refills the digest —
  otherwise 304 (no body) starves it forever (`release.go:60-67`).
- **Hot path stays cache-only:** the nudge reads the digest from
  `inspectCachedRelease` only; no network (`relay health` cache-only comment
  stays true).
- **Session hook (dev-only, contract-tested):** grep-based extraction of the
  highlight `"text"` fields (no jq dependency); same semantic items as the CLI
  paths. Wording aligned with the shared formatter's shape.
- **Fallback:** no valid digest → today's generic version/action notice +
  release URL when available. Empty/malformed/unrecognized bodies fall back
  cleanly; digest composes AROUND the pinned guidance strings (legacy-Windows
  reinstall text, `Run: ha-nova update`), never replaces them.
- **Context skill** (`SKILL.md` → Self-Update): normal HA work stays primary;
  the update surfaces once per session as a clearly separated, localized
  callout after the requested result — version, up to 3 highlight lines,
  release URL, update offer. No auto-install; consent semantics unchanged.
- **docs/releasing.md** (Release Notes Style): note that bullet order expresses
  importance and only recognized sections feed the compact notice.

## Out of scope (per issue)

Snooze/dismiss persistence, reminder frequency, Relay-App release-note UX,
auto-install, per-client hooks, runtime LLM summarization, per-request GitHub
calls, changelog UI. Existing ETag/detached-refresh/throttle/opt-out/dev
suppression/RC-to-stable/exit codes stay intact.

## Tests

- New `cli/release_digest_test.go`: extraction priority, Markdown cleanup,
  1+2/220/700 caps, malformed input, determinism.
- Extend: `update_freshness_test.go` (200 stores digest; 304 keeps it; missing
  digest forces revalidation without `If-None-Match`), `release_test.go`
  (`check-update --json` additive fields, fallback, legacy-Windows guidance
  intact), `update_nudge_test.go` (nudge carries digest, cache-only),
  `prerelease_update_test.go` (RC→stable unchanged), hook + skill contract
  tests (`tests/hooks/session-start.test.ts`, `tests/skills/ha-nova-contract.test.ts`,
  `tests/onboarding/self-update-contract.test.ts`).

## Verification

- `go test ./cli/...`, `npx vitest run` green; `bash scripts/check-docs.sh`.
- Manual: run `check-update --json` against the real cache; verify stdout stays
  machine-clean and stderr carries the notice on a relay call.
- Side-work: safety.md N/A (no safety guarantee change — honesty rules
  untouched), release-body claim, breadcrumbs/choices.
