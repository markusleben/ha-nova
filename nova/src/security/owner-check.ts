import type { IncomingMessage } from "node:http";

import type { RelayLogger } from "../http/server.js";
import { resolveIngressIdentity } from "./ingress-identity.js";

// Server-side owner authorization for the NOVA management page. panel_admin is
// only a UI/access filter, not authorization: a plain admin (or a spoofed direct
// LAN request) must not manage devices. Every management GET and POST re-checks,
// with no cached ownership that could outlive a revoke:
//   1. the request must come through the Supervisor ingress socket peer with the
//      ingress user header (isSupervisorIngressRequest);
//   2. the ingress user must be a real, active, non-system OWNER, verified fresh
//      against config/auth/list over the Supervisor-proxied WS.
// If ownership cannot be determined, deny (503) — never fail open.

export interface HaAuthUser {
  id: string;
  name?: string;
  is_owner?: boolean;
  is_active?: boolean;
  system_generated?: boolean;
  group_ids?: string[]; // system-admin, system-users, system-read-only
}

export type OwnerCheckResult =
  | { ok: true; userId: string; name: string; users: HaAuthUser[] } // users: as read for this check
  | { ok: false; status: 403 | 503; message: string; ownerName?: string | undefined; unknownUser?: true };

export interface OwnerCheckDeps {
  // Fetches config/auth/list via the Supervisor-proxied WS. Throws on failure.
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  logger?: RelayLogger | undefined;
}

export async function checkOwner(
  request: IncomingMessage,
  deps: OwnerCheckDeps,
): Promise<OwnerCheckResult> {
  // Socket peer + ingress headers. A direct LAN request that forges the headers
  // fails here because it does not arrive from the Supervisor ingress peer.
  const identity = resolveIngressIdentity(request);
  if (!identity.ok) {
    return { ok: false, status: 403, message: "Ambiguous ingress user", unknownUser: true };
  }
  const userId = identity.userId;

  let users: HaAuthUser[];
  try {
    users = await deps.fetchAuthUsers();
  } catch (error) {
    // Owner status could not be checked: fail closed, but say why in the log.
    deps.logger?.warn("owner verification unavailable", {
      error: error instanceof Error ? error.message : String(error),
    });
    return {
      ok: false,
      status: 503,
      message: "Owner verification unavailable",
    };
  }

  const isOwner = (u: HaAuthUser) => u.is_owner === true && u.is_active !== false && u.system_generated !== true;
  const user = users.find((u) => u.id === userId);
  if (!user || !isOwner(user)) {
    const owner = users.find(isOwner)?.name; // whom to ask
    return { ok: false, status: 403, message: "Owner access required", ownerName: typeof owner === "string" ? owner.trim() || undefined : undefined };
  }
  return {
    ok: true,
    userId,
    name: typeof user.name === "string" ? user.name : "",
    users,
  };
}
// The approval view's check (gate spec §3, item 2): any active, non-system
// administrator except the user the Relay itself signs in as (a tap or click
// from that user can't be told apart from the Relay). Read fresh each time;
// a read that fails approves nothing (503).
// is_active must be true (not merely absent): an approval needs a positive read.
export function isApprover(user: HaAuthUser, relayUserId: string): boolean {
  return user.id !== relayUserId && user.is_active === true && user.system_generated !== true && (user.is_owner === true || (user.group_ids ?? []).includes("system-admin"));
}

export type ApproverCheckResult = { ok: true; userId: string; name: string } | { ok: false; status: 403 | 503; message: string };

export async function checkApprover(
  request: IncomingMessage,
  deps: OwnerCheckDeps & { relayUserId: () => Promise<string> },
): Promise<ApproverCheckResult> {
  const identity = resolveIngressIdentity(request);
  if (!identity.ok) {
    return { ok: false, status: 403, message: "Couldn't tell who you are. Open NOVA from the Home Assistant sidebar." };
  }
  let users: HaAuthUser[];
  let relayUser: string;
  try {
    [users, relayUser] = await Promise.all([deps.fetchAuthUsers(), deps.relayUserId()]);
  } catch (error) {
    deps.logger?.warn("approver verification unavailable", { error: error instanceof Error ? error.message : String(error) });
    return { ok: false, status: 503, message: "Could not verify who may approve. Try again shortly." };
  }
  const user = users.find((u) => u.id === identity.userId);
  if (user === undefined || !isApprover(user, "")) {
    return { ok: false, status: 403, message: "Only a Home Assistant administrator can approve HA NOVA's requests." };
  }
  if (typeof relayUser !== "string" || relayUser === "") {
    return { ok: false, status: 503, message: "Could not verify who may approve. Try again shortly." };
  }
  if (relayUser === identity.userId) {
    return { ok: false, status: 403, message: "HA NOVA signs in to Home Assistant as you, so your OK can't be told apart from HA NOVA's own. Another administrator can approve it." };
  }
  return { ok: true, userId: identity.userId, name: typeof user.name === "string" ? user.name : "" };
}
