import type { PairingV1Manager } from "../../security/pairing-v1.js";
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
