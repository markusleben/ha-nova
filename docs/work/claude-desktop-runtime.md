# Claude Desktop runtime fallback

Status: active

Follow-up to #869. Scope: macOS Desktop-only setup, without changing which
Claude profile an existing terminal installation uses.

## Contract

- A `claude` on PATH always wins, including its failures. Never retry a failed
  plugin mutation using a different runtime.
- Preserve detection of `~/.local/bin/claude` outside PATH and use that binary
  before considering Desktop's bundle.
- Without one, use the newest working macOS Desktop host binary under
  `~/Library/Application Support/Claude/claude-code/<version>/<checksum>/`.
  Require its completed-download marker and executable, then probe only version
  and plugin help with bounded timeouts. Ignore VM binaries and incomplete or
  unsupported layouts. Keep the standalone install instructions as fallback.
- All plugin operations, verification, repair, removal, and terminal launch
  use the same resolver. Preserve inherited `CLAUDE_CONFIG_DIR` in the launched
  terminal too (resolve relative directories before changing folders); otherwise use
  `~/.claude`. Do not create a PATH shim or modify Desktop settings.
- Desktop's private environment overrides cannot be verified without decrypting
  its settings. When that store exists, doctor warns that Desktop may
  use another profile and explains how to install there explicitly.

## Read-only evidence (2026-10-26)

Desktop 3.6556.2 stores host Claude Code 2.1.308 at
`claude-code/2.1.308/a93ded4b1b47/claude.app/Contents/MacOS/claude`.
`--version` reports `2.1.308 (Claude Code)`; `plugin --help`,
`plugin marketplace --help`, `plugin install --help`, and `plugin update --help`
expose install, update, validate, list, remove, and marketplace add/remove/update.
No plugin mutation was run against the real profile.

Desktop's application code selects a manifest-pinned version/checksum, then an
installed-version fallback. Its host binaries run directly, outside the VM.
The shared Claude Code implementation uses `CLAUDE_CONFIG_DIR` or `~/.claude`.
Desktop's `ccd-environment-config.json` stores `envVars` encrypted with Electron
`safeStorage`; no plaintext config-dir mirror exists. No decryption was attempted.

## Limits and verification

The bundle path is internal: future layouts fail closed to standalone-install
guidance. The newest installed host binary need not be Desktop's pinned binary;
they share plugin state, not a process. App-only environment variables and
encrypted overrides cannot be resolved automatically. Windows Desktop detection
and bundled execution remain unverified and unsupported; its standalone CLI
continues to work.

Tests use fake binaries and temporary homes only. No pairing, service setup,
purge, Cloud, native-keychain tests, or live Home Assistant commands run on macOS.

The runtime-file count stays in one change because lifecycle callers must share
the resolver: most changes replace only a subprocess constructor or PATH check.
