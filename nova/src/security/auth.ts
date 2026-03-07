import { timingSafeEqual } from "node:crypto";

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
  const leftBuffer = Buffer.from(left);
  const rightBuffer = Buffer.from(right);

  if (leftBuffer.length !== rightBuffer.length) {
    return false;
  }

  return timingSafeEqual(leftBuffer, rightBuffer);
}
