import { checkOwner, type OwnerCheckDeps } from "../../security/owner-check.js";
import {
  PAIR_ARM_TTL_MS,
  type PairingV1Manager,
} from "../../security/pairing-v1.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import { requireIngressUser } from "./cloud.js";

export interface PairV2Deps {
  manager: PairingV1Manager;
  relayInstanceId: string;
  relayVersion: string;
}

export function createPairV2InfoHandler(deps: PairV2Deps): RouteHandler {
  return ({ request }) => {
    requireIngressUser(request);
    return {
      relay_version: deps.relayVersion,
      relay_instance_id: deps.relayInstanceId,
      protocol_version: "v2",
      available: true,
    };
  };
}

// POST /pair/v2/arm — Home Assistant sign-in pairing. The owner's own ingress
// session arms a one-time secret that only this user's /pair/v2/start accepts.
// Machine-only: browsers always send Sec-Fetch-Site, and requiring JSON forces
// a CORS preflight, so no web page can arm a pairing for a signed-in owner.
export function createPairV2ArmHandler(
  deps: PairV2Deps & OwnerCheckDeps,
): RouteHandler {
  return async ({ request }) => {
    if (request.headers["sec-fetch-site"] !== undefined) {
      throw new HttpError(
        403,
        "MACHINE_REQUEST_REQUIRED",
        "Browser requests cannot arm pairing",
      );
    }
    const contentType = request.headers["content-type"] ?? "";
    if (contentType.split(";")[0]?.trim().toLowerCase() !== "application/json") {
      throw new HttpError(
        415,
        "UNSUPPORTED_MEDIA_TYPE",
        "Content-Type must be application/json",
      );
    }
    const owner = await checkOwner(request, deps);
    if (!owner.ok) {
      throw owner.status === 503
        ? new HttpError(503, "OWNER_CHECK_UNAVAILABLE", owner.message)
        : new HttpError(403, "OWNER_REQUIRED", owner.message);
    }
    const armed = deps.manager.arm(owner.userId);
    if (armed === null) {
      throw new HttpError(
        503,
        "PAIRING_UNAVAILABLE",
        "The secure device port is not available",
      );
    }
    return {
      pairing_secret: armed.secret,
      expires_in_s: PAIR_ARM_TTL_MS / 1_000,
      relay_instance_id: deps.relayInstanceId,
    };
  };
}

export function createPairV2StartHandler(deps: PairV2Deps): RouteHandler {
  return ({ body, request, response }) => {
    const userId = requireIngressUser(request);
    const result = deps.manager.startCloud(stringField(body, "ke1"), userId);
    if (result.ok) {
      return { handshake_id: result.handshakeId, ke2: result.ke2 };
    }
    if (result.reason === "rate_limited") {
      response.setHeader("retry-after", result.retryAfterSeconds);
      throw new HttpError(
        429,
        "PAIRING_RATE_LIMITED",
        "Too many pairing attempts",
      );
    }
    if (result.reason === "busy") {
      throw new HttpError(
        503,
        "PAIRING_BUSY",
        "Too many concurrent handshakes",
      );
    }
    throw new HttpError(400, "VALIDATION_ERROR", "Invalid pairing request");
  };
}

export function createPairV2FinishHandler(deps: PairV2Deps): RouteHandler {
  return ({ body, request }) => {
    const userId = requireIngressUser(request);
    const result = deps.manager.finishCloud(
      stringField(body, "handshake_id"),
      stringField(body, "ke3"),
      stringField(body, "metadata"),
      userId,
      deps.relayInstanceId,
    );
    if (!result.ok) {
      throw new HttpError(
        401,
        "PAIRING_FAILED",
        "Pairing could not be completed",
      );
    }
    return { response: result.responseB64 };
  };
}

function stringField(body: unknown, key: string): string {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "Request body must be a JSON object",
    );
  }
  const value = (body as Record<string, unknown>)[key];
  if (typeof value !== "string" || value.length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", `Missing field: ${key}`);
  }
  return value;
}
