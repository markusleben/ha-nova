# The tier engine reads YAML thermostats and their template heaters

Status: active

Date: 2026-11-20. Issue MAR-245 (follows MAR-131 and MAR-209). Owner: tier engine (`cli/tier_helper_options.go`, `cli/tier_callset.go`, new `cli/tier_yaml.go`), relay session (`cli/relay_session.go`), and one metadata field in the Relay's `list_dir` (`nova/src/http/handlers/files-ops.ts`). Target: 0.34 (`release/0.34`). High-risk: a new read path in the typed-tier decision, from files Home Assistant loads at startup.

## Today

A plan's service-call set (MAR-127, `cli/tier_callset.go`) decides which `call_service` listeners count. MAR-131 bounds a generic thermostat or hygrostat set up in the UI by reading the switch its options name (`helperCalls`). One set up in YAML has no config entry and no options, so the set goes unknown and every `call_service` listener counts (`callSet.mayFire`).

The maintainer's home (0.33 rc1, read-only) has exactly that:

```
configuration.yaml   climate: !include climate.yaml
                     template: !include_dir_merge_list <dir>     (about 45 files, plus .bak files)
                     homeassistant: packages: !include_dir_named packages
climate.yaml         - platform: generic_thermostat, heater: switch.<heater> (no unique_id)
<dir>/<file>.yaml    - switch: [{name, unique_id, state, turn_on: switch.turn_on <plug>, turn_off: switch.turn_off <plug>}]
```

The heater is a YAML template switch (registry row: platform `template`, its `unique_id`, no config entry), and `template` is on `callingPlatforms`, so even a readable heater would leave the set unknown. With the set unknown, the cover watchdog's `domain: cover` filter can't rule itself out, and its templated cover targets make `plan call light.turn_on` typed. The plan names only the watchdog, not the thermostat behind it. 0.33 ships that as a stated limit (#1250, maintainer decision 2026-11-19).

## Goal

With the App's file access on, the engine reads the YAML Home Assistant is running, bounds YAML generic thermostats and hygrostats and the YAML template switches they drive, and so brings that light to no ask. Every gap stays unknown (today's behavior). When a YAML helper is what leaves the set unknown, the plan names it.

## 1. Proof that the disk is what Home Assistant runs

Home Assistant reads this YAML at start or on a reload; what is on disk now may differ. An mtime proves nothing: `mv` of an older `.bak` over a file keeps the old mtime. The proof uses the inode change time (ctime), which every write, rename, hard link, and copy moves to "now" and which no user call can set back:

- **Relay (metadata only):** each `list_dir` entry gains `changed`, its `lstat` ctime (ISO, `null` for a symlink, as `modified`). No other Relay change; the Relay stays dumb. It ships with the train's App promote. A Relay without the field leaves the set unknown.
- **Start:** Home Assistant's own run lock, `/config/.ha_run.lock` (`{"pid", "version", "ha_version", "start_ts"}`, written at every start before the config loads). A missing, unparsable, or future `start_ts` is unknown.
- **Rule:** every file the projection uses has `changed` before `start_ts`, and every directory a dir include walks has `modified` and `changed` before `start_ts` (so no entry was added, removed, or renamed there since). A `changed` that is missing, zero, before 2000, after the read, or after its own listing's newest `modified` by more than a day is implausible: unknown. Since an inode whose ctime predates the start has kept its name and content since then, Home Assistant read exactly this content at start, and any reload since read it again.
- **Atomicity:** each directory is listed again after its files are read; any change between the two listings is unknown.
- **Consequences:** an edit, a restored backup, or a new `.bak` in an included directory leaves the set unknown until the next restart (the safe direction). The maintainer's `template` directory and every used file currently predate his last start.
- **Proof in the PR:** on the test bed with the real App image (or the HAOS VM), a `mv` of an older `.bak` over a used file and a hard link to one both move `changed` past `start_ts` and leave the set unknown.

## 2. The read (`cli/tier_yaml.go`)

**One internal pseudo-endpoint**, like MAR-131's `helper_options`: `yaml_helpers`, payload exactly `{}`, answered inside the relay session by the resolver below, which sends `files` `list_dir`/`read_file` itself. `checkReadOnlyRequest` admits the pseudo-endpoint and still refuses raw `files`, so no path is persisted in a plan's reads, a cut-plan replay re-runs the resolver live, and the memo holds one answer per session. The answer is the projection only (§3); no file content, path, or Relay or parser error text leaves the session, only fixed codes. In the App's check the resolver's `files` reads are store reads (`gate_verdict.go`), and the Relay's file-access mode and deny list (`secrets.yaml`, `.storage`, its data folder) apply. A denied or disabled read is unknown; a read cut by the session budget is a budget cut, as for any read.

**Loader**, `go.yaml.in/yaml/v3` v3.0.5 (the YAML organization's maintained v3 line; frozen API, security fixes), mirroring Home Assistant's `annotatedyaml` loader (2026.11). It starts at `/config/configuration.yaml` and resolves only the top-level keys `homeassistant` (for `packages`), `climate`, `generic_hygrostat`, `template`, and `switch`, each also as `<key> <label>` (Home Assistant's `^domain(| .+)$` rule), and the includes inside those subtrees. Other keys (`automation: !include automations.yaml`) are never read.

- `!include <file>` is relative to the including file's directory; an empty file is an empty mapping. The dir forms walk recursively with `list_dir`, take `*.yaml` files (case-sensitive), skip names starting with `.` at every level and `secrets.yaml`; `merge_list` keeps lists, `merge_named` mappings, `dir_list` drops empty files, `dir_named` keys by base name.
- `!secret` and `!env_var` become an opaque marker; secrets are never read. A marker in a value the projection uses is unknown; elsewhere it is ignored.
- **Unknown** for: any other tag in a file read; a parse error; more than one document; a duplicate key; a non-literal key where the projection selects (root, `homeassistant`, package names and domains, blocks, entries, steps); an alias or merge key (`<<`) anywhere in a resolved subtree; an include path that is absolute, contains `..` or `%`, or has a symlink as file or as any ancestor directory (each ancestor's listing is checked); a `dir_named` base-name collision or a `merge_named` key collision; a truncated listing (500 entries); more than 50 directories, 2,000 listed entries, 200 files, 8 MiB, include depth 8, or 200,000 YAML nodes; any read error.

## 3. Projection

**Packages.** `homeassistant.packages` is a mapping (inline or via an include) of package name to a mapping of domain keys. The projection takes the union of the root's and every package's domain keys. Home Assistant merges packages and may reject a conflicting one; every rule below turns a superset into "unknown", never into a bound.

**Helpers.** Each `climate` entry with `platform: generic_thermostat` gives its `heater`; each `generic_hygrostat` entry gives its `humidifier`. A domain value is a list or one mapping (`ensure_list`); each value one literal entity id. Anything else is unknown.

**Template switches.** Each `template` block's `switch` entries (a list or one mapping) with their registry unique id: the entry's `unique_id`, prefixed by the block's `unique_id` and `-` when both are set (`template/helpers.py`, `async_setup_template_platform`). An entry in a block that has `triggers`, `trigger`, `actions`, `action`, `conditions`, `variables`, or `trigger_variables` is trigger-based. An entry without `unique_id` is ignored (it has no registry row, so it can't be a heater bounded here); one whose `unique_id` isn't a plain string is unreadable. Legacy `switch` entries with `platform: template` are not projected, but their `switches` entries' `unique_id`s are collected: one equal to the heater's is a duplicate.

**Answer:** per domain, the YAML entries' entity ids (heaters, humidifiers), and for each template heater asked about, its `turn_on`/`turn_off` calls; or one fixed failure code.

## 4. Bounding (`helperCalls`, `cli/tier_helper_options.go`)

Today a helper entity with no config entry ends `helperCalls` as unknown. Instead, when every helper integration involved (`generic_thermostat`, `generic_hygrostat`, and `template` for a template heater) is built in (`manifest/list`, as MAR-131):

1. **Count.** For each domain, the helper entities in `entity/source` with no config entry must number exactly the YAML entries found. Any difference is unknown: a thermostat Home Assistant runs that the engine didn't find, a disabled or failed one. The count is a cross-check on a proven projection (§1), not identity.
2. **Each YAML helper entity** passes today's `quiet`, before anything else, as MAR-131 checks a UI helper.
3. **Each heater** passes today's `plainSwitched` (a registered switch or fan, no group or `switch_as_x`, quiet) plus: its platform is not one the engine treats as a user-written command (`command_line`, `rest`, as `tierEngine.classify`), and no access-capable sibling shares its device (the device-access guard). Then `homeassistant.turn_on`, `homeassistant.turn_off`, and the heater's domain are added. The union of all entries' heaters is bounded.
4. **A template heater** (registry platform `template`, no config entry) is bounded instead of `calledOn`'s `callingPlatforms` rule, only on this helper edge (`calledOn` itself keeps the rule for direct calls): exactly one projected template switch has its registry unique id, it is not trigger-based, no unreadable entry and no legacy duplicate exists. Its `turn_on` and `turn_off` (a list or one mapping, or absent) hold only steps of exactly the shape `{action|service: <domain>.turn_on|turn_off, target: {entity_id: <id or [ids]>}}` (also with `entity_id` at the step instead of `target`, never both), `<domain>` being `switch`, `fan`, or `homeassistant`, with one non-empty list of literal ids; no `data`, no template, no other key. Each target passes step 3's checks (a plain, quiet switch or fan), and for a `switch`/`fan` service lies in that domain. Then each service and each target's domain are added. Anything else is unknown.

Nothing here lowers a tier by itself: it only lets `mayFire` rule a listener out. A UI helper keeps MAR-131's options read; a home with both reads both. `quiet` stays at MAR-131's shipped level (Captain, 2026-11-20): a consumer of a state derived from the plug through a template, and YAML trigger-based template entities with actions, stay the limits `safety.md` already states.

## 5. The plan names the cause

When the YAML path leaves the set unknown, the engine keeps that cause at the helper boundary: the helper entity and one fixed code (`files_off`, `yaml_unread`, `yaml_changed`, `yaml_mismatch`, `heater_not_plain`, `heater_actions`). If a `call_service` listener then counts, the plan lists one reason `HELPER_CALLS_UNREAD` (entity: the helper, detail: the code), rated ordinary itself so it never raises a tier, and a plan that asks says it with the existing unseen sentence (`unseen.what`: "HA NOVA can't see what “Thermostat Badezimmer OG” does"), the name through `sayText`. No new catalog text, no schema field, no plan-file or MCP tool text change.

## Tests

- Go, loader and projection (`cli/tier_yaml_test.go`, a fake file tree behind the fake relay with `changed` times): each include form, nested includes, packages inline and via `!include_dir_named`, `<label>` keys, hidden files and `secrets.yaml` skipped, `!secret` outside a used value ignored; and one unknown case per bullet in §2's list and per rule in §1 (missing `changed`, zero or future ctime, a used file or walked directory changed after start, a listing that changes between reads, no lock).
- Go, engine (`cli/tier_helper_options_test.go`, `cli/tier_unexplained_listener_test.go`): the MAR-209 row with the sanitized shape of the maintainer's home (YAML thermostat → YAML template switch → plug) flips to ordinary with the watchdog not reached, in `plan call` and the App's check (`runTierCases`). Typed, with the `HELPER_CALLS_UNREAD` reason naming the thermostat, for: file access off, a count mismatch, a duplicate unique id (modern or legacy), a trigger-based block, a `data` key or template in the heater action, a cover target, a `command_line` plug, a plug with a lock on its device, a plug, heater, or thermostat something listens to, a non-built-in `template`. A cover call still starts the watchdog.
- Relay (`tests/`): `list_dir` reports `changed` for files and directories and `null` for a symlink.
- Test bed (`scripts/e2e/testbed/calls.mjs`), with `TESTBED_FILES=1` and the real App image: `up.sh` writes a YAML thermostat whose heater is a YAML template switch around a demo switch, before Home Assistant starts. The light row asks nothing, and the YAML files are byte-identical after planning. Then a `mv` of an older copy over the template file, and separately a hard link, make the row typed with the reason naming the thermostat. The default bed (file access off) stays without the YAML helper, so its rows keep their sets. If the bed can't host it without changing another row, the Go row stands alone and the PR says why.

## Build plan

1. This spec (docs PR).
2. The Relay's `changed` field, the loader, and the projection, with their tests.
3. Engine wiring, the cause reason, the MAR-209 row, the bed row; Sol reviews the whole build again with the security/fail-open lens (Captain, 2026-11-20).
4. After the read-only real-home check: `docs/reference/safety.md` drops the 2026-11-19 limit and states the new bound, `.goreleaser.yml` drops the What To Watch bullet, `docs/work/next-release-body.md` names the fix.

Each PR stays under about 400 production lines. The Relay grows by a few lines, under its 13,600 ceiling.

## Review (before build)

Sol (gpt-6.2-sol, high), security/fail-open and KISS lenses, on the first draft. Taken: ctime instead of mtime, with the Relay field (Captain decision A); one internal pseudo-endpoint instead of raw `files` reads; literal include paths and ancestor symlink checks; collisions, aliases, merge keys, and extra documents unknown; the built-in check for all three integrations; the narrow heater action shape; the command and device-access checks on targets; the template exception only on the helper edge; node, directory, and entry caps; the bed proof of a same-count, same-id replacement; cause naming at the helper boundary with the existing sentence instead of an all-sites rewrite and a new schema field; legacy template switches only checked for duplicates. Kept: `quiet` at MAR-131's level (Captain); every planning level makes the read, as MAR-131's options read does, because the code picks every path and the answer is a projection with no configuration text.

## Known limits

- File access off (the App's default): YAML helpers stay unknown, as today; the plan names the helper.
- Any edit, restore, reload, or new file in a used directory since the last start stays unknown until the next restart.
- A YAML template heater without `unique_id`, a legacy template switch as heater, a UI template switch as heater, a heater behind a group or `switch_as_x`, and a heater action beyond plain `turn_on`/`turn_off` on plain switches stay unknown.
- A disabled YAML thermostat fails the count: unknown.
- A filesystem that doesn't move ctime on rename would defeat §1; Home Assistant OS uses ext4, which does, and the bed proof checks it on the shipped image.
