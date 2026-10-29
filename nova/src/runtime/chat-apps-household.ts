import type { HaAuthUser } from "../security/owner-check.js";
import { ChatAppsError } from "./chat-apps-ha.js";

// Who may use chat apps, and how much (docs/work/2026-10-24-chat-apps-
// household-spec.md, "Who can be picked"). The bridge caps every level by
// the user's Home Assistant role on each request (cli/mcp_serve_levels.go);
// this side writes only what the owner picked, never above that cap. The
// owner is always allowed. A list with anyone else is written whole, each
// of them with a level, and with "roles": 1: a 0.27 bridge refuses unknown
// fields, so after a rollback it refuses every chat request instead of
// giving them full power.

export type ChatLevel = "ask" | "use" | "manage";

export const CHAT_LEVELS: readonly ChatLevel[] = ["ask", "use", "manage"];

export interface ChatUsers {
  allowed_users?: string[];
  levels?: Record<string, ChatLevel>;
  roles?: 1;
}

const HA_USER = /^[0-9a-f]{32}$/;

// The most a level gives user: owner or administrator, Manage everything;
// a user, Use the home; any other role (or none), Ask only.
export function roleCap(user: HaAuthUser): ChatLevel {
  if (user.is_owner === true || user.group_ids?.includes("system-admin") === true) {
    return "manage";
  }
  return user.group_ids?.includes("system-users") === true ? "use" : "ask";
}

// A person's own active login, not one Home Assistant made for itself.
export function pickable(user: HaAuthUser): boolean {
  return HA_USER.test(user.id) && user.is_active === true && user.system_generated !== true;
}

// The level a listed user has now: their stored one, lowered to their role's cap.
export function effectiveLevel(user: HaAuthUser, stored: ChatLevel | undefined): ChatLevel {
  const cap = roleCap(user);
  return stored !== undefined && CHAT_LEVELS.indexOf(stored) < CHAT_LEVELS.indexOf(cap) ? stored : cap;
}

// usersToSave: chat-apps.json's list for the owner's picks (user id to
// level), checked against Home Assistant's users as read for this save;
// one user that is unknown, inactive, made by Home Assistant itself, or
// picked above their role refuses the whole save. {}: only the owner (no
// list, so a new owner in Home Assistant still works).
export function usersToSave(ownerId: string, picked: Map<string, string>, users: HaAuthUser[]): ChatUsers {
  const levels: Record<string, ChatLevel> = {};
  for (const [id, level] of picked) {
    if (id === ownerId) {
      continue;
    }
    const user = users.find((u) => u.id === id);
    if (user === undefined || !pickable(user)) {
      throw new ChatAppsError(409, "USER_NOT_PICKABLE", "a picked user is not an active Home Assistant login");
    }
    const at = CHAT_LEVELS.indexOf(level as ChatLevel);
    if (at < 0 || at > CHAT_LEVELS.indexOf(roleCap(user))) {
      throw new ChatAppsError(409, "LEVEL_ABOVE_ROLE", "a picked level is above what the user's Home Assistant role allows");
    }
    levels[id] = level as ChatLevel;
  }
  const others = Object.keys(levels);
  return others.length === 0 ? {} : { allowed_users: [ownerId, ...others], levels, roles: 1 };
}
