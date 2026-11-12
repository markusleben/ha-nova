# System Check-up

"Is my Home Assistant OK?", "why is it slow?", "would my backup work?": one read, every verdict computed by the CLI.

## Read

`ha-nova map --section checkup` (chat apps: `ha_map` with `section: checkup`). Read-only. Never compute a verdict yourself; present `status`, `findings`, `not_checked`, `facts`.

- `status`: `attention` (a problem), `limited` (something could not be read), `ok`. Say `ok` as "nothing found in what HA NOVA checked", never "everything is fine". Notices and unread parts are listed even then.
- `facts.supervisor: not_applicable`: this install type has no Supervisor, so system health, disk, memory and Apps are not available here; never imply they are fine.
- Findings: `problem` first, then `notice`. Each code gets one plain sentence and its next step:
  - `SUPERVISOR_UNHEALTHY`, `SYSTEM_ISSUES`: `fix: repair` → offer the health skill's Fix a Repair flow (its preview and confirmation; nothing is applied before the user confirms). `fix: ui` → Settings > System > Repairs, or Settings > System > System information when no repair shows. Translate the enum words; an unknown one is "something the Supervisor flagged".
  - `DISK_LOW`: `problem` → under 2 GB free, Home Assistant refuses updates and backups. `notice` → space is getting tight. Either way suggest deleting old backups or media in the UI.
  - `HIGH_MEMORY`, `facts.resources`: a reading of this moment, for Home Assistant, the Supervisor and running Apps only. Give the numbers and the top users; never call a number "too high" or name a cause the CLI did not flag.
  - `INTEGRATIONS_FAILING`, `REPAIRS_OPEN`: hand over to this skill's Integrations and Repairs report. `facts.integrations.logging_errors` is a lead (errors since at least `since`), never proof.
  - Backup codes (`NO_BACKUP`, `NO_RECENT_BACKUP`, `BACKUP_LAST_FAILED`, `BACKUP_PROBLEM`, `BACKUP_ONLY_ON_DEVICE`, `NO_AUTOMATIC_BACKUP`): offer `ha-nova:backup`. `BACKUP_LAST_FAILED` is about the last automatic run, even when a newer manual backup exists.
- `not_checked`: each part was not read; never describe it as fine. `SUPERVISOR_REQUEST_REFUSED` → the NOVA App is older than this HA NOVA: update it. `GATE_REFUSED` on `log_facts` → the same. `VIA_HOME_ASSISTANT_LINK` → system details need the home connection. `TOO_MANY_APPS` → only the first 40 running Apps were measured. Any other reason: say it could not be read and offer to check again; guess no cause.
- `facts.updates`: count only; installing is `ha-nova:updates`.

## Would my backup work?

Never answer "yes" or "your backup works". With no backup problem found, say: "no problem found in the newest backup's age, contents and copies; a restore was not tested". Use `facts.backup` and the backup codes: age, whether the database (history) is in it, Apps or folders missing, `off_device_copies`, the automatic schedule. `backups` in `not_checked` → the backups could not be read. Always add:

- HA NOVA did not test a restore (`restore_tested: false`). Age and copies cannot prove a backup restores.
- A protected backup needs its encryption key: keep the emergency kit outside Home Assistant.
- A real proof is a test restore on a spare device, never on this home.

## Output

Short: `Status` (one sentence), `Actions` (problems with their next step), `Notices`, `Not checked`, `Next step`. Plain words per `../ha-nova/output-rules.md`; no codes, ids, or raw JSON.
