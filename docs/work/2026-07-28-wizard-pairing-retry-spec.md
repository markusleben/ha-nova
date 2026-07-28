# Wizard Pairing Retry

## Problem

On a fresh setup, secure pairing currently creates and persists
`client_install_id` while submitting the first six-digit code. If that code is
inactive, rejected, or rate-limited, the Wizard correctly stays on the code
prompt, but its commit-specific config snapshot still describes the file from
before the install ID was persisted. The next code is therefore rejected
locally as a concurrent server-configuration change.

The pairing link also opens the App settings page. That page requires another
manual "Open Web UI" action and always targets the official App slug, even in an
isolated developer build.

A resumed local setup can also finish after repairing connection or client
state without surfacing the existing `ha-nova cloud add` path. When that
command is used directly, OAuth opens in the browser without first explaining
the browser-to-terminal round trip.

## Contract

- Establish and persist `client_install_id` in its own guarded transaction
  before the Wizard opens NOVA or accepts a code.
- Fresh setup atomically creates the previously absent config generation;
  a concurrently created file wins and setup fails without overwriting it.
- Every setup-owned config write uses the exact prior config generation and
  advances the in-memory snapshot only to the bytes it committed.
- Pairing attempts never adopt an unrelated config generation after a write,
  error, interactive prompt, or network response.
- Concurrent config changes before the pairing call remain rejected.
- Concurrent config changes while a pairing network call is in flight are
  rejected before another code or legacy fallback can be submitted.
- Fatal, ambiguous, activation, TLS-pin, and credential-persistence failures
  remain terminal and are not retried.
- Once a finish attempt has an ambiguous outcome, a later typed response cannot
  downgrade the overall result to inactive, rejected, rate-limited, or legacy
  fallback.
- A finish request whose context expires after dispatch remains an unknown
  outcome even though no replay can be attempted on that context.
- DNS, connection, and TLS failures proven to occur before a local or Cloud
  finish request was written stay definitive and are never replayed or
  reported as an unknown outcome.
- A local or Cloud `WroteRequest` callback carrying a write error remains
  pre-dispatch and definitive; only a callback with no write error proves
  dispatch for retry classification.
- The shared finish-retry helper preserves the existing Cloud v2
  `OUTCOME_UNKNOWN` classification and verify-without-retry remediation.
- Secure-v1 fallback to the legacy exchange keeps the refreshed snapshot.
- Official and validated Cloud-development builds open Home Assistant's
  `/app/<slug>` panel directly and retain a visible sidebar/App-page fallback
  for older Home Assistant versions.
- Disabled or unstamped development builds never guess the official App slug;
  they open Home Assistant and require selection of the intended sidebar App.
- A completed local setup that did not already present the connection-mode
  choice shows `ha-nova cloud add` as the optional next step when Cloud support
  is available.
- The completed-install Cloud choice uses a strong semantic section title,
  explains the fallback benefit before storage details, states that local
  access remains preferred, and separates the default-No confirmation with
  whitespace. It adds no decorative box, emoji, or UI dependency.
- Fresh Cloud OAuth announces the browser sign-in and return-to-terminal step
  immediately before opening the browser. It adds no confirmation prompt or
  extra setup state.

## Verification

- Regression tests cover inactive, rejected, rate-limited, and v1-fallback
  results after the pre-code `client_install_id` transaction.
- Concurrency tests cover fresh-ID and existing-ID configuration drift during a
  pairing call, absent-config creation, both existing-config install-ID commit
  windows, and successful pairing saves.
- Finish replay tests cover an ambiguous first outcome followed by a typed
  definitive response, a proven pre-dispatch transport failure, and a failed
  request write.
- Cloud finish tests cover persistent ambiguity, mixed definitive responses,
  proven pre-dispatch transport failure, and context expiry after dispatch.
- URL tests cover official, isolated developer, and disabled/unstamped routes.
- UX tests cover the optional post-setup Cloud command and the pre-browser
  OAuth explanation, plus the Cloud-choice hierarchy and default-No prompt.
- Run the targeted Go test package and one isolated real Wizard pairing.
