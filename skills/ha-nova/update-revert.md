# HA NOVA Update-Revert Execution

## Three recovery layers, never conflated

- **Update-revert checkpoint** (this file): client-local, per server and
  domain+target, ONE step back to the last verified update; a same-target
  update replaces it. `ha-nova snapshot show --list` shows what is still
  revertible (alias, saved time, server).
- **Config snapshot** (`../ha-nova/config-snapshots.md`): captured before
  covered destructive HA NOVA operations; restores the deleted/overwritten
  item itself.
- **Home Assistant Backup**: full system recovery, the safety net when
  neither targeted layer covers the case.

Load this file whenever the user asks to revert, undo, or restore a verified
update — any of those intents, not only the literal word `revert`.

## Automation / script / dashboard: `ha-nova plan revert`

`ha-nova plan revert [--target <config_id>] [--domain automation|script|dashboard]`
(a dashboard's target is its `url_path`)
plans the newest checkpoint of this server (or the named target) as a normal
update: show its `diff` and behavior narrative, take natural confirmation,
`ha-nova apply --plan <id>`. The checkpoint is the **only** source of `before_config` — never reconstruct the previous config from memory. A revert
is checkpointed too, so a second revert reverts the revert.
- `REVERT_DRIFT` (exit 2): the item changed since its last verified update
  (e.g. an edit in the HA UI); nothing was planned. Do NOT overwrite blindly:
  show the live config and ask what the user wants written instead.
- `NO_UNDO_SNAPSHOT`: nothing revertible for that target on this server.

## Manual capture (helper storage family)

Manual capture (after a verified helper update): write the record with the
client's file tool and run `ha-nova snapshot save --data-file <record-file>`:

```json
{"op":"update","domain":"<helper type>","target_id":"<helper id>","name":"<the item's alias/friendly name>",
 "server":"<server profile on multi-server installs>","before_config":{ ... },"expected_after":{ ... }}
```

`before_config` = the complete pre-write read-back, `expected_after` = the
complete post-write verified read-back (never the draft). Do **not** reduce it to core fields. Repeat the printed receipt (`created`/`replaced`, `evicted[]`).

Revert: `ha-nova snapshot show --target <target_id>` (add `--domain <d>` when ids collide) is the only
`before_config` source; re-read the live list item into a file and run
`ha-nova snapshot verify --target <id> --against <live-file>` with the same
selection (a bare `verify` checks the NEWEST snapshot). `drift` (exit 2) → do
NOT overwrite; show what differs and ask. `match` → rebuild a schema-valid
`{type}/update` payload from `before_config` (set `{type}_id` from its stored
`id`, keep only writable fields — `../ha-nova/helper-schemas.md` → Common
Rules), preview, confirm, write, re-read to confirm. Config-entry helpers have
no auto-revert — point to HA Backups.

## Honesty

- The store holds the last 25 updated targets per server; older entries are gone, and a
  re-update of a target replaces that target's snapshot (one step back per
  target, never two).
- An automation/script revert restores the checkpoint's bytes exactly; a
  helper revert rebuilds the payload. For a guaranteed point-in-time restore,
  Home Assistant Backups is the source of truth.
