import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import type { PairingManager } from "../../security/pairing.js";

const PAIRING_CODE_PATTERN = /^\d{6}$/;

export function createPairHandler(pairing: PairingManager): RouteHandler {
  return ({ body, request, response }: RouteContext) => {
    const code = parsePairingCode(body);
    const peer = request.socket.remoteAddress ?? "unknown";
    const result = pairing.exchange(code, peer);

    if (result.ok) {
      return { relay_token: result.relayToken };
    }

    if (result.reason === "rate_limited") {
      response.setHeader("retry-after", result.retryAfterSeconds);
      throw new HttpError(429, "PAIRING_RATE_LIMITED", "Too many pairing attempts");
    }

    throw new HttpError(401, "PAIRING_FAILED", "Pairing code is invalid or expired");
  };
}

function parsePairingCode(body: unknown): string {
  if (!isPlainObject(body)) {
    throw validationError();
  }

  const keys = Object.keys(body);
  if (
    keys.length !== 1 ||
    keys[0] !== "code" ||
    typeof body.code !== "string" ||
    !PAIRING_CODE_PATTERN.test(body.code)
  ) {
    throw validationError();
  }

  return body.code;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validationError(): HttpError {
  return new HttpError(400, "VALIDATION_ERROR", "Request body must contain one six-digit code");
}
