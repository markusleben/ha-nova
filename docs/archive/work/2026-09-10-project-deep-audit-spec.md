# Project Deep Audit — 2026-09-10

Status: merged — findings landed through the 2026-09-11 pull-request train below (archived with the closing docs PR).

## Goal

Audit the current repository for correctness, maintainability, fail-fast
behavior, duplicate logic, retry/idempotency risks, and missing Home Assistant
skill capabilities. Prefer the smallest high-impact fixes.

## Scope

- Relay runtime and Census Worker
- Go CLI, installers, CI, and release tooling
- All 31 skills, shared skill references, tests, and active documentation
- Home Assistant 2026.9 and current HACS drift since the last full screen

## Method

1. Map current architecture and run the canonical host-safe verification.
2. Run three read-only specialist audits: Relay/runtime; CLI/release; skills/docs.
3. Reproduce every surviving finding against the current head.
4. Rank findings by real impact and propose surgical fixes; do not apply them
   without a separate user decision.

## Completion

- Every reported finding has current file/line evidence and practical impact.
- Cross-component duplicates are deduplicated.
- Home Assistant capability gaps distinguish shipped APIs from speculative work.
- The final plan is ordered by impact and smallest safe change.

## Executive Summary

The repository is broadly healthy: the complete verification suite, Go race
detector, Go vet, and both production dependency audits pass. The audit found
five urgent reliability/security gaps, nine important correctness/coverage
gaps, and eight bounded maintenance gaps; none requires a new dependency or a
new architecture layer.

## Executive To-Do

- [x] P0: pin every action used by the image publisher; scan the Go CLI with
  fail-closed CodeQL. — #626 (round 1, gate exception) + #625 (round 2).
- [x] P0: bound the HA WebSocket authentication handshake and close abandoned
  auto-reconnecting connections. — #627.
- [x] P0: reject overly permissive private credential files. — #628 (same-owner
  0644 repaired to 0600, foreign owner or unrepairable mode fails closed).
- [x] P1: support Home Assistant 2026.9 child-device shape and inherited area.
  — #629 (joins #520).
- [x] P1: retire the template-reference snapshot instead of restoring its
  refresh path; the review loads templating guidance conditionally. — #635.
- [x] P1: keep pairing replies generic but log persistence failures; make
  malformed HA/Supervisor responses fail at the source. — #628, #630.
- [x] P1: fix the Codex watchdog trigger and testing guide code fence. — #625,
  #634.
- [x] P2: make context budgets count unconditional transitive references.
  — #631 (route ratchets, #521).
- [ ] P2: Census schema 1 — deliberately deferred (see Resolution, finding 15).
- [x] P2: clean Windows updater artifacts (#632), snapshot quota concurrency
  (#633), strict port parsing (#630), freshness wording and completed
  work-spec lifecycle (#634).

## Findings

### Urgent

1. **9/10 — Mutable GitHub Actions execute with package-publish authority.**
   `.github/workflows/relay-image.yml:128-200` uses mutable major tags for
   checkout, registry login, QEMU, Buildx, and image push while the job holds
   `packages: write`. The existing pin verifier rejects this workflow as an
   unknown scope. A moved upstream tag could access the package token and
   publish arbitrary relay images. Smallest fix: pin all workflow actions to
   full commit SHAs with exact version comments; extend the existing pin
   verifier to every action-bearing workflow, with the publisher first.

2. **8/10 — CodeQL omits the security-critical Go CLI and fails open.**
   `.github/workflows/codeql.yml:25-43` scans only JavaScript/TypeScript and
   marks analyzer failure successful with `continue-on-error`. The unscanned
   production Go CLI is 55,146 lines and owns pairing, secrets, update, and
   uninstall. Smallest fix: add Go, remove the failure bypass, and pin the
   actions while touching the workflow.

3. **8/10 — HA WebSocket authentication can hang forever.**
   `nova/src/ha/socket.ts:39-124` has no handshake deadline. The downstream
   request timeout in `nova/src/ha/ws-client.ts:98-100,135,276-327` starts only
   after connection, so one silent handshake leaves the shared
   `connectingPromise` pending and wedges every Relay WS call. Smallest fix:
   one timer in `createAuthenticatedHaSocket`, cleared on every settle path,
   with a silent-server regression test.

4. **7/10 — Failed HA connections stay alive and auto-reconnect.**
   `nova/src/ha/ws-client.ts:115,234,329-332` drops only the reference;
   `nova/src/runtime/start.ts:288-297` omits the installed library's `close()`
   method. New requests create another connection while the old one keeps
   reconnecting. Smallest fix: expose `close()` on `HaWsConnection`, close
   before clearing it, and assert one close in the existing reset test.

5. **7/10 — Private credential files are accepted when world-readable.**
   `nova/src/storage/atomic-file.ts:19-24,84-116` writes `0600` but reads a
   regular `0644` file without rejection. Callers include the pinned-TLS key,
   device registry, pairing response store, and relay instance identity.
   Smallest fix: reject group/other permission bits during private-file reads
   and add one `0644` regression case.

### Important

6. **8/10 — Home Assistant 2026.9 child devices are not modeled.**
   `skills/health/SKILL.md:68-88`,
   `skills/ha-nova/capability-answer.md:62-65`,
   `skills/health/availability-analysis.md:185-188`, and
   `skills/fallback/relay-ready.md:107-110` drop `parent_device_id`, do not
   inherit a parent's area, and still use the legacy `config_entries` array for
   one resolution path. Child rows omit that array. Results can lose room
   membership or fail integration resolution. Smallest fix: one shared
   child-aware normalization rule using singular `config_entry_id`, plus a
   mixed parent/child fixture. This joins existing issue #520.

7. **7/10 — Template-reference refresh is broken and the stale copy is always
   loaded.** `scripts/update-ha-template-reference.mjs:7-8` fetches a removed
   upstream URL (HTTP 404); `docs/reference/ha-template-reference.md:1-3` is
   frozen at 2026-03-04; `skills/review/SKILL.md:195-209` requires it for every
   review. Home Assistant split the source into `source/_docs/templating*`.
   Smallest fix: update the generator to the current source set, validate
   required anchors, and load it only when a reviewed config contains Jinja.

8. **7/10 — Pairing persistence failures look like bad credentials and leave
   no operator signal.** `nova/src/security/pairing-v1.ts:297-329` collapses
   registry capacity and durable-write exceptions into `invalid`; the V1/V2
   handlers return only generic pairing failure. The external response should
   remain generic, but disk-full/read-only/corruption failures must be logged.
   The same logging gap exists in owner actions at
   `nova/src/http/handlers/nova-page.ts:135-148`.

9. **6/10 — Malformed successful HA JSON becomes successful `null`.**
   `nova/src/ha/rest-client.ts:144-153` converts invalid JSON to `{status: 200,
   body: null}` and the test explicitly pins it. This moves a proxy/upstream
   failure away from its cause. Smallest fix: throw `HaRestClientError` for
   invalid JSON content and reverse that regression expectation.

10. **6/10 — Malformed owner lookup becomes a false authorization denial.**
    `nova/src/runtime/ingress-routes.ts:194-200` converts a non-array
    `config/auth/list` response to `[]`; `owner-check.ts:45-63` then reports 403
    instead of its designed retryable 503. Smallest fix: validate and throw on
    the response shape.

11. **7/10 — The label-gated Codex watchdog posts an inert trigger.**
    `.github/workflows/pr-review-watchdog.yml:82-101` posts a multiline
    `@codex fix ...` comment, while this repository's bot accepts only a bare,
    single-line `@codex`. Smallest fix: post context first, then a separate bare
    trigger; add a static contract assertion.

12. **6/10 — Testing prose is inside an executable Bash block.**
    `docs/reference/testing.md:50-66` places the production Census safety rule
    inside the isolated-CLI snippet. Copy/paste aborts on the leading `-` before
    the intended pairing/setup/uninstall commands. Smallest fix: split the code
    fence and validate shell snippets in the existing docs check.

13. **6/10 — Home Assistant 2026.9 administrator requirements are absent from
    the update skill.** `skills/updates/SKILL.md:61-87` does not explain or
    classify non-admin rejection for install/skip/clear-skip. App mode uses a
    Supervisor system context and remains clean; standalone relays configured
    with a non-admin LLAT can fail opaquely. Add the precondition and error
    classification; no Relay logic is needed.

14. **5/10 — Skill word budgets omit mandatory transitive context.**
    `skills/ha-nova/SKILL.md:240-243`, `skills/review/SKILL.md:195-209`, and
    `tests/skills/skill-word-budget-contract.test.ts:436-485` yield a mandatory
    static review route of about 31.8k words while the green budget test treats
    output rules, checks, and template reference as standalone. Smallest fix:
    traverse unconditional reads in the oracle; load output rules once per
    session; make checks/template sections conditional. The write route also
    still requires at least 3,340 lines and remains correctly tracked by #521.

### Bounded maintenance

15. **5/10 — Census schema-1 mutations have no request budget.**
    `census-worker/src/rate-limit.ts:37-42` deliberately returns no identity;
    `census-worker/src/census.ts:326-332` increments the public legacy counter
    for every retry. The 256-row bound limits keys, not request volume, CPU, or
    counter inflation. This needs a backwards-compatibility decision: retire
    schema 1 or add a coarse privacy-preserving/global budget.

16. **5/10 — Supervisor response corruption fails soft.**
    `nova/src/ha/supervisor-client.ts:66-76,113-123` turns malformed network
    fields or ports into an empty/null mapping, which can silently suppress the
    pinned-TLS listener. Validate object shape and integer ports 1..65535;
    distinguish absent from malformed.

17. **4/10 — Windows updates leave temporary executables and failed staging
    bundles.** `cli/command_update.go:107-121,181-255,303-316` never self-deletes
    `%TEMP%/ha-nova-updater-<pid>.exe`; launch failure returns before staged
    cleanup. Reuse the uninstall helper's existing `--self-path` deletion path
    and clean both artifacts on launch failure.

18. **4/10 — Snapshot hard quotas race.**
    `nova/src/http/handlers/backups.ts:272-305` checks file count/bytes before
    asynchronous creation without reservation, so concurrent saves can both
    pass. Serialize the process-local check-and-create section and add one
    two-request test.

19. **4/10 — Health reports a broken snapshot store as healthy and empty.**
    `nova/src/http/handlers/health.ts:50-57` swallows every summarization error
    and returns zero files/bytes. Keep health live, but expose an unavailable
    flag or log the failure; do not represent an I/O fault as valid empty data.

20. **4/10 — Freshness policy text contradicts shipped behavior.**
    `docs/reference/2026-03-01-agent-best-practice-freshness-patterns.md:20-25`
    blocks all failed refreshes; `skills/ha-nova/best-practices.md:19-26,
    163-166` and `write-safety.md:31-36` use the current tiered policy and
    disagree on whether simple continuation warns. Keep one tiered, silent
    runtime rule and mark the older reference historical.

21. **3/10 — Configuration parsing accepts malformed ports.**
    `nova/src/config/env.ts:63-66` parses `8791oops` as 8791 and `1.5` as 1.
    Replace `parseInt` with `Number` before the existing integer/range guard.

22. **3/10 — Completed work specs remain active.**
    The multi-server, HACS lifecycle, and first-hour specs still declare
    `Status: active` although issues #411, #478, and #528 are closed. This
    conflicts with `docs/reference/documentation-governance.md:51-67` and can
    mislead agents. Archive them; keep the August skill audit active because its
    issue set still has open work.

## The Plan

1. **PR A — CI trust boundary:** pin Actions, cover all workflows in the pin
   oracle, add Go to fail-closed CodeQL, repair the watchdog trigger. Highest
   supply-chain and automation value; no new tooling.
2. **PR B — WS lifecycle:** add one handshake deadline and one close path in the
   shared client, with two focused tests. Removes both connection leaks at the
   root.
3. **PR C — private-state safety:** enforce file mode on reads and add operator
   logging without changing pairing responses. High-risk; keep separate. Before
   implementation, choose the compatibility behavior for existing `0644`
   files: fail closed or repair only an owner-matching regular file to `0600`.
4. **PR D — HA 2026.9 skill drift:** child-aware device normalization, update
   permission wording, fixtures, and API matrix. Joins #520 where applicable.
5. **PR E — fail-fast transport:** reject malformed HA JSON, owner lists, and
   Supervisor network mappings at their shared parsers.
6. **PR F — context/reference cleanup:** repair the templating source generator,
   make large review inputs conditional, then make the budget oracle count the
   true transitive load. Builds on #521; do not add another context system.
7. **Small independent fixes:** docs fence, Windows helper cleanup, snapshot
   serialization, strict port parse, freshness wording, and spec archival.
8. **Decision item:** schema-1 Census compatibility. Do not change behavior
   until the backwards-compatibility choice is explicit.

## Verification Evidence

- Full verification (`npm run verify`): green.
- Counts: 1,328 core tests, 45 installer tests, 127 onboarding tests, build,
  566 release-contract tests, and all Go tests pass.
- `go test -race ./...`: green.
- `go vet ./...`: green.
- Root and Relay production npm audits: zero vulnerabilities.
- `npm run verify:docs`: green, demonstrating the reported docs gaps are not
  currently covered.

## Clean Areas

- Pairing cryptography/replay binding, schema-2 Census idempotency and limits,
  registry atomic writes, listener separation, redirect blocking, file-path
  containment, release evidence binding, updater checksum/rollback, uninstall
  checkpoints, and English-only skill contracts are sound in the examined
  paths.
- No unsafe blind retry/double-fire path was found in calendar, HACS, backup,
  maintenance, to-do, or external-call flows.
- Skill inventory/routing is complete: one context router plus 30 task skills.
- Previously reported service-call deferral and media-source-search gaps are
  fixed. Composite-split handling and the review-check executable oracle remain
  correctly tracked by #520/#521 rather than duplicated here.

## Resolution (2026-09-11 pull-request train)

| Finding | Pull request | Merge commit |
|---|---|---|
| 1, 2, 11 | #625 (pins, CodeQL Go); #626 closed and the watchdog removed with the gates in #646 | 9b48e68f |
| 3, 4 | #627 | 7dc43d29 |
| 5, 8 | #628 | 4a4afd0e |
| 6, 13 (+ API matrix, drift log) | #629 | be7ef216 |
| 9, 10, 16, 19, 21 | #630 | cdfaac36 |
| 14 | #631 (route ratchets) → #635 (context reduction, also finding 7) | cf89fd06, a9b0ebaa |
| 17 | #632 | 751fc822 |
| 18 | #633 | 9e290001 |
| 12, 20, 22 | #634 | 20a2a7f7 |
| 15 | superseded: the Census feature is scheduled for removal (maintainer decision 2026-10-14). Original deferral:  schema 2 ships since client v0.21.3 (#437), but `version.json` declares no client floor, so schema 1 stays accepted and counted; removal waits for an explicit backwards-compatibility decision that pins a client floor of at least 0.21.3. No IP capture and no global attacker-blockable limiter were added. | — |

Review depth per AGENTS.md: 2–3 adversarial subagents on the high-risk PRs
(#625, #626, #627, #628, #632, #633), one on #630 and #635, none on the
skills/tests/docs PRs; the local Codex CLI review ran for #625–#630 until its
usage quota ended.
