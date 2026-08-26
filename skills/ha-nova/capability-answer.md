# Capability Answer, Home Overview, Safety Story

Owned by the context skill. These three beginner-shaped questions get direct
answers grounded in THIS home — never a feature-list dump, never a pointer to
documentation.

## Capability Answer — "What can you do?"

Answer in everyday jobs, not skill names, and ground it in the actual home
with ONE bounded aggregate read:

1. `ha-nova relay core --method GET --path /api/states --out <result-file>`,
   then `ha-nova relay jq --file <result-file> --jq-file <filter-file>` to
   count entities per domain (never print the raw dump — it may carry
   coordinates and presence).
2. Render a short List Frame grouped by everyday jobs, each line naming what
   exists HERE, for example:
   - Control: lights, switches, covers, climate, media (with the counts found)
   - Automate: create/change automations, scenes, schedules, helpers
   - Watch: history, energy, cameras, who is home, what is open or running
   - Organize & maintain: rooms/areas, names, updates, backups, cleanups
   - Voice: what Assist understands, teach it new phrases
3. Skip groups with zero matching entities; name up to two things the home
   does NOT have wired yet (e.g. no energy sources configured) as honest
   scope, not as failure.
4. Close with a Next step inviting ONE concrete job ("Want a tour? Ask:
   show me my home"). The Suggestion Block caps apply.

## Home Overview — "Show me my home"

Aggregate counts are List-Frame-legal; entity dumps are not.

- One `/api/states` pull with `--out`, counts via `relay jq` — never print or
  read the raw dump (reuse the Capability Answer read when it happened this
  session); plus the area registry for room counts.
- Report: rooms/areas, entities per domain (top groups only), how many
  automations/scenes/scripts exist, and current activity (lights on, media
  playing, anyone home) as counts — not as an entity listing.
- Never include coordinates, tracker positions, or per-person locations
  unless the user asked for exactly that.
- Close with one Next step (e.g. the Structure Check below — owned here,
  fixes hand to `ha-nova:organize` — or starter proposals per
  `starter-proposals.md`).

## Structure Check — "Is my setup tidy?"

Read-only audit from bounded registry reads (entity registry + area
registry), owned here; every FIX hands to `ha-nova:organize`'s normal
preview/confirm flow:

- count entities without an area, and names still carrying integration
  defaults (device-model strings, `_2` suffixes); report counts plus a short
  sample, never the full listing.
- close with the fixes as a legal Next step ("say 'move them to rooms' and
  I'll take them through ha-nova:organize, one preview each").

## Safety Story — "Can I break something?"

Render the ENFORCED guarantees user-facing, on request only, in about five
lines:

- Nothing is written without a preview you confirm first; confirmations bind
  to exactly what was shown.
- Deleting an automation, scene, helper, dashboard, or similar config needs
  a typed confirmation code — a plain "yes" is never enough there.
- Automation, script, and storage-helper updates keep a revert path; deleted
  items in snapshot-covered families restore from automatic config snapshots.
- Reads are bounded; credentials and secrets never appear in chat.
- When something is outside these guarantees (scene, dashboard, energy, and
  calendar writes have no revert; config-entry helpers have no snapshot), the
  owning skill names the limit — the honest limit is part of the story.
