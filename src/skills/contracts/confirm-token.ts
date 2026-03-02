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

export type WriteConsentTier = "read" | "create_update" | "destructive";

export type WriteConfirmationMode = "none" | "natural" | "token";

export type WriteConfirmationReason =
  | ConfirmValidationReason
  | "preview_id_mismatch"
  | "token_required"
  | "confirmation_not_accepted";

export interface WriteConfirmationContext extends ConfirmValidationContext {
  tier: WriteConsentTier;
  previewId: string;
  expectedPreviewId: string;
  token?: ConfirmTokenRecord;
}

export interface WriteConfirmationResult {
  ok: boolean;
  mode?: WriteConfirmationMode;
  reason?: WriteConfirmationReason;
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

const NATURAL_CREATE_UPDATE_CONFIRMATIONS = new Set<string>([
  "ja",
  "ja bitte",
  "ja bitte erstellen",
  "bitte erstellen",
  "erstellen",
  "mach das",
  "apply",
  "go ahead",
  "proceed",
]);

function normalizeConfirmation(confirmation: string): string {
  return confirmation
    .trim()
    .toLowerCase()
    .replace(/[.!?]+$/g, "")
    .replace(/\s+/g, " ");
}

function isTokenConfirmation(confirmation: string): boolean {
  return /^confirm:[^:\s]+$/.test(confirmation.trim());
}

export function isNaturalCreateUpdateConfirmation(confirmation: string): boolean {
  return NATURAL_CREATE_UPDATE_CONFIRMATIONS.has(normalizeConfirmation(confirmation));
}

function fromTokenFailure(tokenResult: ConfirmValidationResult): WriteConfirmationResult {
  return {
    ok: false,
    reason: tokenResult.reason ?? "confirmation_not_accepted",
    ...(tokenResult.remediation ? { remediation: tokenResult.remediation } : {}),
  };
}

export function validateWriteConfirmation(
  confirmation: string,
  context: WriteConfirmationContext
): WriteConfirmationResult {
  if (context.previewId !== context.expectedPreviewId) {
    return {
      ok: false,
      reason: "preview_id_mismatch",
    };
  }

  if (context.tier === "read") {
    return { ok: true, mode: "none" };
  }

  if (context.tier === "create_update") {
    if (context.token && isTokenConfirmation(confirmation)) {
      const tokenResult = validateAndConsumeConfirmToken(confirmation, context.token, context);
      if (!tokenResult.ok) {
        return fromTokenFailure(tokenResult);
      }

      return {
        ok: true,
        mode: "token",
        consumedTokenId: context.token.tokenId,
      };
    }

    if (isNaturalCreateUpdateConfirmation(confirmation)) {
      return {
        ok: true,
        mode: "natural",
      };
    }

    return {
      ok: false,
      reason: "confirmation_not_accepted",
    };
  }

  if (!isTokenConfirmation(confirmation)) {
    return {
      ok: false,
      reason: "token_required",
    };
  }

  if (!context.token) {
    return {
      ok: false,
      reason: "token_required",
    };
  }

  const tokenResult = validateAndConsumeConfirmToken(confirmation, context.token, context);
  if (!tokenResult.ok) {
    return fromTokenFailure(tokenResult);
  }

  return {
    ok: true,
    mode: "token",
    consumedTokenId: context.token.tokenId,
  };
}
