# Next release — draft body

Status: active

Version-free by design; the release-prep PR renames this file once it picks
the number.

## What To Watch

- **Cloud Remote is macOS-only in this release.** The validated platform
  list shrank to `darwin` (2026-08-19): the Windows and Linux validation
  machines are no longer available, and Cloud publication stays gated on
  per-platform proof. Windows and Linux clients keep full local
  functionality; their Cloud Remote option returns once validation
  infrastructure is available again. Existing older installs are unaffected.

## Internal

- Runtime dependency train: `ws` 8.21.3 (root + nova), `jose` 6.2.8 (#588).
- Evidence contract: ledger-recorded reference-smoke waiver for unavailable
  validation infrastructure (#592).
