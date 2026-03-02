export interface ConfirmTokenRecord {
  tokenId: string;
  issuedAtMs: number;
  method: string;
  path: string;
  target: string;
  previewDigest: string;
}

export interface ConfirmValidationContext {
  nowMs: number;
  ttlMs: number;
  method: string;
  path: string;
  target: string;
  previewDigest: string;
  usedTokenIds: Set<string>;
}

export type ConfirmValidationReason =
  | "format_invalid"
  | "token_mismatch"
  | "stale"
  | "replay"
  | "binding_mismatch_method"
  | "binding_mismatch_path"
  | "binding_mismatch_target"
  | "binding_mismatch_digest";

export interface ConfirmValidationResult {
  ok: boolean;
  reason?: ConfirmValidationReason;
  consumedTokenId?: string;
  remediation?: {
    regeneratePreview: boolean;
    issueFreshToken: boolean;
  };
}

function fail(reason: ConfirmValidationReason): ConfirmValidationResult {
  return {
    ok: false,
    reason,
    remediation: {
      regeneratePreview: true,
      issueFreshToken: true,
    },
  };
}

export function validateAndConsumeConfirmToken(
  confirmation: string,
  token: ConfirmTokenRecord,
  context: ConfirmValidationContext
): ConfirmValidationResult {
  const [prefix, suppliedTokenId, ...rest] = confirmation.split(":");
  if (prefix !== "confirm" || !suppliedTokenId || rest.length > 0) {
    return fail("format_invalid");
  }

  if (suppliedTokenId !== token.tokenId) {
    return fail("token_mismatch");
  }

  if (context.nowMs > token.issuedAtMs + context.ttlMs) {
    return fail("stale");
  }

  if (context.usedTokenIds.has(token.tokenId)) {
    return fail("replay");
  }

  if (context.method !== token.method) {
    return fail("binding_mismatch_method");
  }

  if (context.path !== token.path) {
    return fail("binding_mismatch_path");
  }

  if (context.target !== token.target) {
    return fail("binding_mismatch_target");
  }

  if (context.previewDigest !== token.previewDigest) {
    return fail("binding_mismatch_digest");
  }

  context.usedTokenIds.add(token.tokenId);
  return { ok: true, consumedTokenId: token.tokenId };
}

export function validateConfirmToken(
  confirmation: string,
  token: ConfirmTokenRecord,
  context: ConfirmValidationContext
): ConfirmValidationResult {
  return validateAndConsumeConfirmToken(confirmation, token, {
    ...context,
    usedTokenIds: new Set<string>(context.usedTokenIds),
  });
}
