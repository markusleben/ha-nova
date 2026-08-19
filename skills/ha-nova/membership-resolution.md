# Membership Resolution

Canonical path: `skills/ha-nova/membership-resolution.md`

Shared contract for resolving the membership of a dynamic or composite
target — a group, an area, a device, a presence-routed household — BEFORE
the action preview. A composite target's preview binds to its resolved
members, never to the collection name alone: acting on a name whose members
nobody read changes recipients, side effects, verification scope, and safety
classification without the user seeing the affected set.

## Authoritative membership sources

Every composite type has exactly one authoritative source; when it cannot be
read, the membership is unknown — never substituted with a guess:

| Composite type | Authoritative source |
|---|---|
| Modern group helpers (light, switch, cover, fan, binary_sensor, ...) | config-entry options; the `entity_id` state attribute is the fallback read only when the options are unreadable — when both are readable and disagree, that is source disagreement (below): re-resolve, never pick one silently |
| Legacy `group.*` | `attributes.entity_id` where exposed; otherwise uninspectable |
| Notify groups | UI-built notify group entities resolve like modern group helpers (row 1); a legacy notify group SERVICE exposes no readable membership — `/api/services` proves it exists, never who it reaches: treat it as uninspectable |
| Media player groups | the `group_members` state attribute |
| Areas and devices | entity/device registry plus `search/related` on the resolved area (`skills/ha-nova/bulk-patterns.md`) |
| Presence-routed household | `person.*` states mapped to real notify targets (`skills/notify/SKILL.md` → Household routing) |

## Nested expansion

A member can itself be a group. Expand recursively, depth-bounded at 5:
deeper nesting than any real home uses is not resolvable with confidence, so
a branch still composite at the bound is UNRESOLVED, never silently
truncated. A member already visited on the current path is a cycle — stop
that branch there; a cycle between fully read members is resolved, not an
error. Remove duplicates keeping the first occurrence, and preserve
deterministic ordering: the source order of each authoritative read,
parents before their expanded children — in the PREVIEW. A frozen executable
payload contains only the deduplicated concrete leaf entities, never a group
parent beside its own children: the parent's fan-out plus direct child calls
would actuate members twice (a toggle can end where it started).

## Four resolution outcomes

Every resolution ends in exactly one of:

- **fully resolved** — every member enumerated from its authoritative source
- **partially resolved** — some members enumerated, some branches unreadable
- **uninspectable** — the source exposes no membership at all
- **changed after preview** — the pre-execution re-read differs from the previewed list

Source disagreement — two readable sources with different member sets — is
PARTIALLY RESOLVED at best: show both readings, act on neither, and re-resolve
before any preview binds.

The preview shows every resolved member the action affects. Partially
resolved and unresolvable members are shown explicitly, named as unread.
An unresolvable group is NEVER reported as empty: "could not read" and
"contains nothing" are different answers and only one of them is true. Fail
honestly — name what could not be resolved and stop — rather than acting on
guessed recipients or members.

## Freeze or re-read

Two execution modes, decided by semantics:

- **Freeze:** where calling the members directly is semantically equivalent
  to calling the collection (area/device expansion, entity groups whose
  service fans out identically), execute with the resolved `entity_id` list
  in the payload. The previewed membership is frozen: a member added or
  removed after the preview is neither silently actuated nor silently
  skipped.
- **Re-read:** where direct member calls would change the semantics (media
  grouping mechanics, notify group fan-out, presence routing), keep the
  collection target but re-read its membership immediately before
  execution. ANY change that alters recipients, side effects, or risk
  invalidates the existing confirmation and produces a new preview — the
  confirmation bound to the list that was shown.

## Safety and verification

Safety classification uses the highest-risk resolved member: a group is as
dangerous as the most dangerous thing in it, and an unreadable member
escalates rather than defaulting down (`skills/ha-nova/indirect-actuation.md`
→ rule 3). When the action promises member-state changes, verification
covers the expanded members, not the collection entity.

## Existing instances

- `skills/service-call/SKILL.md` Flow step 3: area/device expansion with the
  frozen member list.
- `skills/ha-nova/indirect-actuation.md`: legacy `group.*` targets forward
  to their members and classify recursively.
- `skills/notify/SKILL.md` → Household routing: presence re-read immediately
  before sending; its unreadable-presence handling stays authoritative.

## Out of scope

Creating or editing groups; expanding scenes, scripts, and automations
(`skills/ha-nova/indirect-actuation.md` owns indirect actuation); replacing
a group call with member calls when semantics differ; claiming notification
delivery.
