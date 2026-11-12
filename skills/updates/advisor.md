# Update advisor

Run before every install, one update or a batch, and when the user asks whether they can update.

1. `ha-nova map --section updates`. It only reads; it saves nothing in Home Assistant.
2. Present it plainly (`../ha-nova/output-rules.md`: no codes or ids):
   - `ready: no`: name each blocker and its fix; install nothing it blocks.
     - `SUPERVISOR_UNHEALTHY`: Home Assistant refuses Core and OS updates until the system is healthy again (Settings > System > Repairs).
     - `DISK_LOW`: under 2 GB free, Home Assistant refuses updates; free space first.
     - `UPDATE_RUNNING`: wait until that update finishes.
     - `APP_NEEDS_NEWER_CORE`: that App needs a Home Assistant version not offered yet; it waits.
   - Warnings: `NO_RECENT_BACKUP` or `BACKUP_ONLY_ON_DEVICE` → offer a backup that leaves the device (`ha-nova:backup`) before Core, OS, or Apps; `BACKUP_RUNNING` → wait for it; `REPAIRS_OPEN` → name the alerts by title and offer to look first (`ha-nova:health`).
   - `not_checked`: say which checks could not run and why.
   - Never say an update is safe. `ready: yes` means: no blockers found in the checks HA NOVA made.
   - Timing: suggest a time when someone is home and no backup runs; Core and OS restart Home Assistant, firmware can take long.
3. Order: follow `order` (`backup` = offer the safety backup first). It is a default (`order_basis: default`); a prerequisite the release notes name comes first. `after_core` Apps need the new Core.
4. Every install keeps its own preview, gates, and confirmation (`SKILL.md` → Install). The advisor adds facts and an order, never a confirmation: "update everything" is still the explicitly confirmed batch.
