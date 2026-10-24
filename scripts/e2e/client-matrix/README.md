# Client release matrix

A re-runnable, client-side release regression run: the public one-liner
install of the last stable tag, setup against the local test bed, client wiring,
the update to the release candidate, and uninstall — per platform lane, in
throwaway HOMEs.

```bash
bash scripts/e2e/client-matrix/run.sh --from v0.25.1 --to local     # candidate = this checkout's HEAD
bash scripts/e2e/client-matrix/run.sh --from v0.25.0 --to v0.25.1   # published tag to published tag
bash scripts/e2e/client-matrix/run.sh --from v0.25.1 --to local --lanes linux --keep
```

Prints a PASS/FAIL/SKIP table per lane, scenario, and step; exits non-zero on
any FAIL (or a lane without results). The work dir (logs per step) is kept on
failure or with `--keep`. Runtime: ~2 min for a cold local build, then
~1 min per lane.

Prerequisites: Docker, curl, `column`; for `--to local` also Go, Node,
Python 3, git, jq, zip, and bash >= 4 on `PATH` (the release bundle script uses
`mapfile` and zips the Windows bundle). A published `--to` tag must be newer
than `--from`, so the update really swaps the runtime. A crashed lane adds a
FAIL row even when earlier rows passed.

## Safety

- **Local test bed only.** An existing `ha-nova-testbed` compose project is
  reused only when it was started from this checkout and relay and HA are
  healthy, and it is never torn down. A bed from another tree (another
  agent's, or an older Relay) or an unhealthy one stops the run.
  With none present, `run.sh` starts one from this checkout and runs `down.sh`
  at exit once its `up.sh` succeeded. The only other network peer is GitHub (installer, release
  bundles, release API).
- **Throwaway HOME per scenario** with every `XDG_*` dir inside it,
  `env -i` (no inherited `CLAUDE_CONFIG_DIR`, tokens, or client config), and
  `SHELL` set per scenario, so the installer's PATH edit lands in the
  throwaway `.zshrc` / `.profile`.
- **No keychain/Secret Service.** The relay token goes to a test keyring file
  (`HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1` + `HA_NOVA_TEST_KEYRING_FILE`, plus
  a unique `HA_NOVA_KEYRING_SERVICE`). On macOS each HOME is also pre-seeded
  with the device-credential file-backend marker, which keeps the throwaway
  install's own device credential in a file. That is **not** keychain
  isolation: the keychain belongs to the OS user, not to HOME. When the login
  keychain holds any `ha-nova.device-credential*` item (checked by name with
  `security dump-keychain`, no secrets read), macOS B SKIPs `purge`,
  `purge-leftovers`, and `purge-rc`; the Linux lane keeps covering purge. For
  a macOS purge run, use a separate macOS user account or a VM.
- Linux runs in a fresh `ubuntu:24.04` container as an unprivileged user.

## What each lane runs

| Scenario | Shell / rc | Token path | Steps |
| --- | --- | --- | --- |
| A | zsh / `.zshrc` | test keyring file | install `--from` → `setup codex`, `setup opencode` (+ `setup claude` when a `claude` binary exists) → `doctor`, `relay health`, read (`find --domain light`, or `relay core GET /api/` before `find` existed) → skill trees resolve into the installed bundle → `mcp` (initialize, tools/list, `ha_find`) when the version has it → update → config fields, stored token, server profile, client wiring, `clients_verified_version` survived → relay-outdated notice asserted against `min_relay_version` vs the test bed relay → `doctor`, read, `mcp` → `uninstall --yes` → leftovers, rc block, relay still up |
| B | bash / `.profile` | macOS: test keyring file; Linux: real headless `setup --service hermes` (token file) | install → setup → clients → update → survived → `doctor` → `uninstall --yes --purge` → leftovers, rc block, relay still up (macOS: purge steps SKIP when the login keychain holds HA NOVA credentials) |

Client runtimes (`codex`, `opencode`, `hermes`) are no-op stubs on `PATH`:
setup only checks they exist, and the lanes assert the files HA NOVA wires.
Claude Code runs for real when `claude` is installed on the macOS host, with
`CLAUDE_CONFIG_DIR` inside the throwaway HOME; the Linux lane skips it.

Each rc file is seeded with a sentinel line before install; after uninstall
the file and the sentinel must remain and the managed PATH block must be gone.
The Hermes check covers every namespaced skill dir (`ha-nova`,
`ha-nova-<skill>`) against the installed bundle, and after the update that
each file was rewritten.

Leftover rule after uninstall: no file or link in HOME whose path or content
names `ha-nova`, except the documented keep-list — `--yes` keeps
`~/.config/ha-nova/{config.json,relay-token,secrets/.file-backend}` and both
modes keep the opaque `~/.config/.ha-nova-*` lifecycle markers.

## `--to local`

`build-local.sh` builds release-shaped bundles from committed `HEAD`
(uncommitted edits are not included): `git archive` into a temp copy, stamp the
next patch version with `scripts/bump-version.sh` when `version.json` is not
newer than `--from`, build all five targets with the release flags
(`-tags cloudremote_official`, `-X main.Version`), and package them with
`scripts/release/build-install-bundle.sh`. Cloud evidence is signed with a
throwaway key, so Cloud Remote stays off in these bundles. The bundles are
served from `127.0.0.1` and installed the supported way:
`HA_NOVA_BUNDLE_URL=<bundle> ha-nova update --version <stamped version>`
(same override `install.sh` honors).

## Shells

- zsh → `~/.zshrc`; bash → `~/.bash_profile` if present, else `~/.profile`
  (installer and `uninstall` agree).
- fish is not covered by a lane (`installer-contract.test.ts` and
  `cli/path_manage_test.go` cover it): the installer writes
  `${XDG_CONFIG_HOME:-~/.config}/fish/conf.d/ha-nova.fish`, unknown shells get
  `~/.profile`, the target is recorded in `~/.config/ha-nova/path-target`, and
  `uninstall` also sweeps every file older installers could have written.

## Windows

No local lane; the release workflows cover Windows on native runners:

- `release-candidate.yml` → `smoke-bundles` (windows-latest): RC bundle
  extract, version match, `ha-nova version`, `internal-cloud-release-check`.
- `release.yml` → `smoke-release-bundles` (windows-latest): same on the draft
  release assets.
- `release.yml` → `smoke-installers` (windows-latest, post-publish):
  `install.ps1`, `version`, `check-update --quiet`, same-version
  `update --version`, `uninstall --yes`, then waits for the background
  uninstall (install root + `uninstall-status.json` gone).
- `ci.yml`: `windows-native-secret-boundary`, `windows-powershell-unicode`.

Not covered on Windows by any automated job: setup against a relay, doctor,
client wiring, MCP, update from the previous stable, `--purge`. Manual scripts:
`scripts/dev/windows-*.ps1` (`npm run test:desktop:windows:*`).
