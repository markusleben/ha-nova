import { createHash, timingSafeEqual } from "node:crypto";

export interface AuthSuccess {
  ok: true;
}

export interface AuthFailure {
  ok: false;
  status: 401;
  code: "UNAUTHORIZED";
  message: string;
}

export type AuthResult = AuthSuccess | AuthFailure;

export function authorizeRequest(
  authorizationHeader: string | undefined,
  expectedToken: string
): AuthResult {
  if (!authorizationHeader) {
    return unauthorized("Missing authorization header");
  }

  const [scheme, token] = authorizationHeader.split(" ", 2);
  if (scheme !== "Bearer" || !token) {
    return unauthorized("Invalid bearer token");
  }

  if (!constantTimeEqual(token, expectedToken)) {
    return unauthorized("Invalid bearer token");
  }

  return { ok: true };
}

function unauthorized(message: string): AuthFailure {
  return {
    ok: false,
    status: 401,
    code: "UNAUTHORIZED",
    message
  };
}

function constantTimeEqual(left: string, right: string): boolean {
  // Hashing both sides first makes the comparison length-independent — the
  // old early return on length mismatch leaked the token length via timing.
  const leftDigest = createHash("sha256").update(left).digest();
  const rightDigest = createHash("sha256").update(right).digest();
  return timingSafeEqual(leftDigest, rightDigest);
}
