import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import type { PairingV1Manager } from "../../security/pairing-v1.js";

// The OPAQUE pairing bootstrap, served bearer-exempt on the plain HTTP port
// (8791). The one-time code IS the credential; these endpoints never reveal the
// code, the pairing status, or any Home Assistant detail. /info is a static
// capability probe so an old CLI can detect an incompatible relay.

export interface PairV1Deps {
  manager: PairingV1Manager;
  relayVersion: string;
}

export function createPairV1InfoHandler(deps: PairV1Deps): RouteHandler {
  return () => ({
    relay_version: deps.relayVersion,
    protocol_version: "v1",
    available: true,
  });
}

export function createPairV1StartHandler(deps: PairV1Deps): RouteHandler {
  return ({ body, request, response }: RouteContext) => {
    const ke1 = stringField(body, "ke1");
    const result = deps.manager.start(ke1, canonicalPeer(request));
    if (result.ok) {
      return { handshake_id: result.handshakeId, ke2: result.ke2 };
    }
    switch (result.reason) {
      case "rate_limited":
        response.setHeader("retry-after", result.retryAfterSeconds);
        throw new HttpError(429, "PAIRING_RATE_LIMITED", "Too many pairing attempts");
      case "busy":
        // Only reachable once handshakes exist, which needs an active code and a
        // valid KE1 — so it is not a cheap oracle for the pairing window, and a
        // real "server busy" signal helps a legitimate client retry.
        throw new HttpError(503, "PAIRING_BUSY", "Too many concurrent handshakes");
      default:
        // "inactive" collapses into this generic response on purpose: the
        // bootstrap port is unauthenticated on the LAN, so a distinct "no active
        // code" reply would let any client detect when the owner's pairing window
        // is open. The CLI shows a "get a fresh code" hint for this 400.
        throw new HttpError(400, "VALIDATION_ERROR", "Invalid pairing request");
    }
  };
}

export function createPairV1FinishHandler(deps: PairV1Deps): RouteHandler {
  return ({ body, request }: RouteContext) => {
    const handshakeId = stringField(body, "handshake_id");
    const ke3 = stringField(body, "ke3");
    const metadata = stringField(body, "metadata");
    const result = deps.manager.finish(handshakeId, ke3, metadata, canonicalPeer(request));
    if (result.ok) {
      return { response: result.responseB64 };
    }
    // Generic on every failure: wrong code, tampering, replay, and expiry are
    // indistinguishable to the caller.
    throw new HttpError(401, "PAIRING_FAILED", "Pairing could not be completed");
  };
}

// The rate-limit and peer-binding identity is the canonical socket address, with
// the IPv4-mapped IPv6 prefix stripped so ::ffff:1.2.3.4 and 1.2.3.4 are one
// peer. Forwarded headers are never trusted.
function canonicalPeer(request: RouteContext["request"]): string {
  const addr = request.socket.remoteAddress ?? "unknown";
  return addr.startsWith("::ffff:") ? addr.slice("::ffff:".length) : addr;
}

function stringField(body: unknown, key: string): string {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must be a JSON object");
  }
  const value = (body as Record<string, unknown>)[key];
  if (typeof value !== "string" || value.length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", `Missing field: ${key}`);
  }
  return value;
}
