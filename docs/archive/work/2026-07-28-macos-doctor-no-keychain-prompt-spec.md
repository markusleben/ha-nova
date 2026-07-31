# macOS Doctor Without Keychain Prompts

## Problem

In a macOS build that does not meet the hardened Cloud secure-storage
boundary, device-credential and legacy relay-token reads fall back to
`go-keyring`. Its `/usr/bin/security` subprocess may open a Keychain password
dialog even when the caller selected `SecretStoreForbidUI`. Background Doctor
and session-start checks can therefore queue repeated password dialogs.

## Contract

- Every macOS Keychain operation reached by Doctor with
  `SecretStoreForbidUI` disables Keychain interaction before access.
- If access would require a password or approval, return a classified error
  immediately; never show UI.
- In development/ad-hoc builds, restore the prior process Keychain interaction
  setting after the bounded in-process operation.
- Serialize the temporary process-wide interaction-policy change.
- Use the same native Keychain caller for newly written secrets and later
  non-interactive reads.
- Never broaden an existing item's ACL. If another executable identity owns a
  legacy or test item, Doctor fails without UI; recovery is an explicit
  interactive setup/re-pair with the installed signed HA NOVA build. Promotion
  recreates the current device item under that identity instead of preserving
  the foreign ACL.
- Permit Keychain UI only for operations that explicitly use
  `SecretStoreAllowUI`, such as interactive setup. Background commands never
  retry a denied operation interactively, including relay-token writes,
  rollbacks, and cleanup.
- Continue decoding legacy `go-keyring` hex/base64 envelopes.

## Verification

- Regression tests prove Cloud-disabled macOS reads and Doctor-reachable
  mutations use the native path and do not call `go-keyring`.
- macOS package tests install the in-memory `go-keyring` provider during
  package initialization, before `TestMain` or any helper subprocess can run.
- Tests cover success, missing items, interaction-required errors, restoration,
  serialized access, context cancellation, mutation uncertainty, relay-token
  access, matching read/write identity, and restoration-error preservation for
  the in-process path.
- Native macOS CI runs the no-UI and interactive-pairing regressions while
  sandbox policy denies `/usr/bin/security`.
- Run targeted Darwin tests, the full Go suite, and onboarding contracts in an
  environment that cannot invoke the user's globally installed HA NOVA; local
  macOS verification also denies execution of `/usr/bin/security`.
