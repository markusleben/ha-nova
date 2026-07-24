# Census Consent UX

Status: implemented, unreleased

## Goal

Make the shipped census disclosure technically honest and require a distinct,
unambiguous consent choice in AI clients.

## Contract

- Describe the fixed JSON body separately from HTTPS transport metadata.
- Introduce Cloudflare as the hosting provider for the census endpoint, then
  state that it processes the source IP and connection metadata under its
  privacy policy.
- State only the enforceable HA NOVA guarantee: Worker code does not read the
  source IP, and application storage/public statistics do not store it.
- Do not call the census or ping anonymous. Describe the application payload
  as identifier-free instead.
- Present one standalone choice with three effects: contribute, do not
  contribute, or show the exact data without changing consent.
- Keep the visible disclosure to one heading and at most five short lines
  before the three actions.
- Use plain weekly and message-content language in the consent surface. Keep
  attempt/ISO-week/application-body accounting terms in the technical details
  only.
- Use a native selectable menu when available and the shared numbered fallback
  otherwise. If a client requires a default, use the privacy-safe opt-out.
- A missing or ambiguous answer changes nothing. Showing details changes
  nothing and re-renders the same choice.
- The terminal fallback uses the same strict numbered three-action choice;
  free-form input never counts as Yes or No.
- Never stack the census choice with another active menu, write preview,
  runtime-action confirmation, or destructive confirmation code. Defer it to
  the next conflict-free response, and let the census choice close that
  response.
- Count only conflict-free user-visible presentations toward the three-display
  cap; a delivered machine notice that is deferred consumes nothing.
- Keep the existing wire schema, endpoint, public commands, local state,
  weekly gate, and immediate eligible attempt after opt-in unchanged.

## Verification

- Pin CLI copy and existing consent/send behavior in Go tests.
- Pin the standalone three-option interaction and deferral rule in skill
  contract tests.
- Positively allowlist census Worker Request reads through TypeScript AST and
  symbol inspection; reject aliases, computed access, and `arguments`.
- Reject ambiguous anonymous/no-IP claims on active census surfaces.
- Run targeted tests, `npm run verify`, and `go test ./...`.
- Build a release-like local CLI in an isolated temporary home and inspect the
  real TTY prompt plus `census status` output before commit or PR.
