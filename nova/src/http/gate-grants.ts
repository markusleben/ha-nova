import { createHash, randomBytes } from "node:crypto";

import type { GateEndpoint } from "./gate.js";

// Grants (docs/work/2026-11-03-relay-typed-gate-spec.md §4): an approval
// becomes a grant of the asked bytes' digests, each usable once by the device
// that asked, for two minutes. Memory only: a restart drops them.

export const GRANT_MS = 120_000;

interface Grant {
  owner: string;
  until: number;
  unused: string[]; // one per request, a repeated request once per occurrence
}

export interface GateGrants {
  issue(owner: string, digests: string[]): { grantId: string; until: number };
  // Marks one occurrence used before the request is forwarded (at most once).
  consume(owner: string, grantId: string, endpoint: GateEndpoint, body: Buffer): boolean;
  prune(): void;
}

export function gateDigest(endpoint: GateEndpoint, body: Buffer): string {
  return createHash("sha256").update(endpoint).update(Buffer.from([0])).update(body).digest("hex");
}

// Ids are looked up by their hash, so a lookup's timing says nothing about an id.
export const idKey = (id: string): string => createHash("sha256").update(id).digest("hex");

export function createGateGrants(now: () => number): GateGrants {
  const grants = new Map<string, Grant>();
  return {
    issue(owner, digests) {
      const grantId = randomBytes(32).toString("base64url");
      const until = now() + GRANT_MS;
      grants.set(idKey(grantId), { owner, until, unused: [...digests] });
      return { grantId, until };
    },
    consume(owner, grantId, endpoint, body) {
      const id = idKey(grantId);
      const grant = grants.get(id);
      if (grant === undefined || grant.owner !== owner || now() > grant.until) {
        return false;
      }
      const at = grant.unused.indexOf(gateDigest(endpoint, body));
      if (at === -1) {
        return false;
      }
      grant.unused.splice(at, 1);
      if (grant.unused.length === 0) {
        grants.delete(id);
      }
      return true;
    },
    prune() {
      const at = now();
      for (const [id, grant] of grants) {
        if (at > grant.until) {
          grants.delete(id);
        }
      }
    },
  };
}
