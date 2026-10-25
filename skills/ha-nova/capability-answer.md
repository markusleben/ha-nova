# Capability Answer, Home Overview, Safety Story

Owned by the context skill. These three beginner-shaped questions get direct
answers grounded in THIS home — never a feature-list dump, never a pointer to
documentation.

## Capability Answer — "What can you do?"

Answer in everyday jobs, not skill names, and ground it in the actual home
with ONE bounded aggregate read:

1. `ha-nova map` — its `domains` are the exact entity counts per domain
   (never count by hand, and never print the raw dump of any read — states
   carry coordinates and presence).
2. Render a short List Frame grouped by everyday jobs, each line naming what
   exists HERE, for example:
   - Control: lights, switches, covers, climate, media (with the counts found)
   - Automate: create/change automations, scenes, schedules, helpers
   - Watch: history, energy, cameras, who is home, what is open or running
   - Organize & maintain: rooms/areas, names, updates, backups, cleanups
   - Voice: what Assist understands, teach it new phrases (first use may
     need Relay file access enabled — the owning skill guides that)
3. Skip groups whose ENABLING hardware is absent — Automate stays visible
   whenever ANY automatable signal exists: controllable devices, or sensors/
   calendars/presence that can drive notifications, helpers, and schedules
   (zero existing automations is an invitation, not absence); name up to two things the home does NOT have
   wired yet as honest scope, not as failure — and only claims
   the session's reads actually prove ("no energy dashboard configured" needs
   the energy prefs read, never a guess from entity absence).
4. Close with a Next step inviting ONE concrete job ("Want a tour? Ask:
   show me my home"). The Suggestion Block caps apply.

## Claude's Question — "Why does Claude ask every time?"

In a chat app, that question comes from Claude, not from HA NOVA. Answer
with both cases: in Claude, open Customize → Connectors → HA NOVA, then

- with a linked phone: set every tool to "Always allow". HA NOVA still
  previews bigger changes, and the phone asks before unlocks, the alarm, and
  deletes.
- with no phone: set every tool to "Always allow" except "Run a confirmed
  change": leave it on "Needs approval", so Claude still asks before a
  change confirmed with a code (the AI can see that code). With a phone
  linked, that one can be allowed too.

Claude may ask again after an update.

## Home Overview — "Show me my home"

Owned by `ha-nova:home-map` (What do I have), which reads `ha-nova map`.
Aggregate counts are List-Frame-legal; entity dumps are not. Never include coordinates, tracker positions, or per-person locations
unless the user asked for exactly that.

## Structure Check — "Is my setup tidy?"

Read-only audit from bounded registry reads (FULL entity registry — the
compact list lacks `original_name` — plus the device and area registries),
owned here; every FIX hands to `ha-nova:organize`'s normal
preview/confirm flow:

- count AREA-ASSIGNABLE entities without an EFFECTIVE area: device-bound
  ones (an empty entity `area_id` inherits the device's area — join the
  device registry first; a child device's null `area_id` inherits its
  parent's, `../ha-nova/membership-resolution.md` → Device rows) AND
  device-less entities that accept an
  entity-level `area_id` (template sensors and the like); persons,
  automations, scripts, scenes, and other non-room service entities (sun,
  zones, update/assist engines) legitimately have no area and are never
  counted as untidy.
- name provenance: an entity whose registry `name` is null runs on its
  integration default (`original_name`) — that IS the provenance signal.
  Mere equality with a device model, or a bare `_2` suffix, reports as
  "possibly default", never as a rename offer. Counts plus a short sample,
  never the full listing.
- close with the fixes as a legal Next step ("say 'move them to rooms' and
  I'll take them through ha-nova:organize, one preview each").

## Safety Story — "Can I break something?"

Render the ENFORCED guarantees user-facing, on request only, in about five
lines:

- Nothing is written without a preview you confirm first; confirmations bind
  to exactly what was shown (secret values stay masked and bind to the
  stored value behind the mask).
- Deleting things (an automation, scene, helper, dashboard) and big actions
  like unlocking a door or opening the garage need you to confirm yourself.
  A plain "yes" in chat is never enough. Often a window on your computer or
  your linked phone asks you. Otherwise you type a four-digit one-time code
  from HA NOVA.
- Automation, script, dashboard-content, and storage-helper updates keep a revert path; deleted
  items in snapshot-covered families restore from automatic config snapshots.
- Reads are bounded; pairing and authentication secrets never appear in
  chat, and webhook trigger ids are masked even in raw YAML you explicitly
  ask for.
- When something is outside these guarantees (scene, energy, and
  calendar writes have no revert; config-entry helpers have no snapshot), the
  owning skill names the limit — the honest limit is part of the story.
