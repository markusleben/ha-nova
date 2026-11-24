import type { IncomingMessage } from "node:http";

import type { PageKey } from "../http/page-texts.js";
import type { AuthOutcome, RelayLogger } from "../http/server.js";
import type { DeviceRegistry } from "./device-registry-types.js";
import { resolveIngressIdentity } from "./ingress-identity.js";

// Server-side authorization for the NOVA management page. panel_admin is
// only a UI/access filter, not authorization: a non-admin (or a spoofed direct
// LAN request) must not manage devices. Every management GET and POST re-checks,
// with no cached role that could outlive a change:
//   1. the request must come through the Supervisor ingress socket peer with the
//      ingress user header (resolveIngressIdentity);
//   2. the ingress user must be a real, active, non-system OWNER (or, where the
//      route allows it, ADMINISTRATOR: docs/work/2026-11-12-nova-page-admins-spec.md),
//      verified fresh against config/auth/list over the Supervisor-proxied WS.
// If the role cannot be determined, deny (503) — never fail open.

export interface HaAuthUser {
  id: string;
  name?: string;
  is_owner?: boolean;
  is_active?: boolean;
  system_generated?: boolean;
  group_ids?: string[]; // system-admin, system-users, system-read-only
}

export type OwnerCheckResult =
  | { ok: true; userId: string; name: string; users: HaAuthUser[]; isOwner: boolean } // users: as read for this check
  | { ok: false; status: 403 | 503; message: string; ownerName?: string | undefined; unknownUser?: true };

export interface OwnerCheckDeps {
  // Fetches config/auth/list via the Supervisor-proxied WS. Throws on failure.
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  logger?: RelayLogger | undefined;
}

// config/auth/list over the Supervisor-proxied WS. A non-list reply is a
// Supervisor/HA fault, not "no owner": it throws, so a check answers 503.
export async function readAuthUsers(ws: { sendMessage(message: { type: string }): Promise<unknown> }): Promise<HaAuthUser[]> {
  const result = await ws.sendMessage({ type: "config/auth/list" });
  if (!Array.isArray(result)) throw new Error("config/auth/list returned a non-list result");
  return result as HaAuthUser[];
}

// A computer an administrator connected (pairedBy) works only while that
// person is still an active owner or administrator, read fresh on every
// request (MAR-170); a failed read refuses. The credential is resolved again
// after the read, so a revoke meanwhile still wins.
export async function checkPairedBy(resolve: () => AuthOutcome, registry: Pick<DeviceRegistry, "list">, fetchAuthUsers: () => Promise<HaAuthUser[]>): Promise<AuthOutcome> {
  const first = resolve();
  const principal = first.ok ? first.principal : undefined;
  const maker = principal?.kind === "device" ? registry.list().find((d) => d.deviceId === principal.deviceId)?.pairedBy : undefined;
  if (maker === undefined) return first;
  const user = await fetchAuthUsers().then((users) => users.find((u) => u?.id === maker) ?? null).catch(() => undefined);
  if (user === undefined) return { ok: false, status: 503, code: "ROLE_UNKNOWN", message: "HA NOVA can't check right now whether the administrator who connected this computer still is one" };
  if (user === null || !isAdmin(user)) return { ok: false, status: 401, code: "UNAUTHORIZED", message: "The administrator who connected this computer no longer manages this Home Assistant. Connect it again with a new code." };
  return resolve();
}

// An active, non-system owner. is_active must be true (a positive read).
export function isOwner(user: HaAuthUser): boolean {
  return user.is_owner === true && user.is_active === true && user.system_generated !== true;
}

// An active, non-system administrator: the owner, or a member of
// system-admin. A role HA NOVA can't read (group_ids not a list) is none.
export function isAdmin(user: HaAuthUser): boolean {
  return isOwner(user) || user.is_active === true && user.system_generated !== true && Array.isArray(user.group_ids) && user.group_ids.includes("system-admin");
}

// admins: the route also lets administrators in (isOwner tells them apart).
export async function checkOwner(
  request: IncomingMessage,
  deps: OwnerCheckDeps,
  options: { admins?: boolean } = {},
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

  const user = users.find((u) => u.id === userId);
  if (!user || !(options.admins === true ? isAdmin(user) : isOwner(user))) {
    const owner = users.find(isOwner)?.name; // whom to ask
    return { ok: false, status: 403, message: "Owner access required", ownerName: typeof owner === "string" ? owner.trim() || undefined : undefined };
  }
  return {
    ok: true,
    userId,
    name: typeof user.name === "string" ? user.name : "",
    users,
    isOwner: isOwner(user),
  };
}
// The approval view's check (gate spec §3, item 2): any active, non-system
// administrator except the user the Relay itself signs in as (a tap or click
// from that user can't be told apart from the Relay). Read fresh each time;
// a read that fails approves nothing (503).
// is_active must be true (not merely absent): an approval needs a positive read.
export function isApprover(user: HaAuthUser, relayUserId: string): boolean {
  return user.id !== relayUserId && isAdmin(user);
}

// A refusal names its page text (page-texts.ts); the page words it.
export type ApproverCheckResult = { ok: true; userId: string; name: string } | { ok: false; status: 403 | 503; text: PageKey };

export async function checkApprover(
  request: IncomingMessage,
  deps: OwnerCheckDeps & { relayUserId: () => Promise<string> },
): Promise<ApproverCheckResult> {
  const identity = resolveIngressIdentity(request);
  if (!identity.ok) {
    return { ok: false, status: 403, text: "page.approver_unknown" };
  }
  let users: HaAuthUser[];
  let relayUser: string;
  try {
    [users, relayUser] = await Promise.all([deps.fetchAuthUsers(), deps.relayUserId()]);
  } catch (error) {
    deps.logger?.warn("approver verification unavailable", { error: error instanceof Error ? error.message : String(error) });
    return { ok: false, status: 503, text: "page.approver_unavailable" };
  }
  const user = users.find((u) => u.id === identity.userId);
  if (user === undefined || !isApprover(user, "")) {
    return { ok: false, status: 403, text: "page.approver_not_admin" };
  }
  if (typeof relayUser !== "string" || relayUser === "") {
    return { ok: false, status: 503, text: "page.approver_unavailable" };
  }
  if (relayUser === identity.userId) {
    return { ok: false, status: 403, text: "page.approver_relay_user" };
  }
  return { ok: true, userId: identity.userId, name: typeof user.name === "string" ? user.name : "" };
}
