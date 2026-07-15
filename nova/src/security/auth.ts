import { constantTimeEqualSecret } from "./secret-compare.js";

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

  if (!constantTimeEqualSecret(token, expectedToken)) {
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
