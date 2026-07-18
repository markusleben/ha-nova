import type { IncomingMessage } from "node:http";

import { isSupervisorIngressRequest } from "../http/handlers/home.js";

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
}

export type OwnerCheckResult =
  | { ok: true; userId: string; name: string }
  | { ok: false; status: 403 | 503; message: string };

export interface OwnerCheckDeps {
  // Fetches config/auth/list via the Supervisor-proxied WS. Throws on failure.
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
}

export async function checkOwner(request: IncomingMessage, deps: OwnerCheckDeps): Promise<OwnerCheckResult> {
  // Socket peer + ingress headers. A direct LAN request that forges the headers
  // fails here because it does not arrive from the Supervisor ingress peer.
  if (!isSupervisorIngressRequest(request)) {
    return { ok: false, status: 403, message: "Ingress request required" };
  }
  const userId = singleUserId(request);
  if (userId === null) {
    // Missing or multiple X-Remote-User-Id.
    return { ok: false, status: 403, message: "Ambiguous ingress user" };
  }

  let users: HaAuthUser[];
  try {
    users = await deps.fetchAuthUsers();
  } catch {
    // Owner status could not be checked: fail closed.
    return { ok: false, status: 503, message: "Owner verification unavailable" };
  }

  const user = users.find((u) => u.id === userId);
  if (!user || user.is_active === false || user.system_generated === true || user.is_owner !== true) {
    return { ok: false, status: 403, message: "Owner access required" };
  }
  return { ok: true, userId, name: typeof user.name === "string" ? user.name : "" };
}

function singleUserId(request: IncomingMessage): string | null {
  const raw = request.headers["x-remote-user-id"];
  // Node joins duplicate headers with ", "; reject anything ambiguous.
  const value = Array.isArray(raw) ? null : raw;
  if (typeof value !== "string" || value.length === 0 || value.includes(",")) {
    return null;
  }
  return value;
}
