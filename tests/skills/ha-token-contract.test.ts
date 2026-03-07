import { describe, expect, it } from "vitest";

import {
  type ConfirmTokenRecord,
  validateAndConsumeConfirmToken,
  validateConfirmToken,
  validateWriteConfirmation,
} from "../../nova/src/skills/contracts/confirm-token.js";

describe("ha token contract", () => {
  const baseToken: ConfirmTokenRecord = {
    tokenId: "tok-123",
    issuedAtMs: 1_700_000_000_000,
    method: "POST",
    path: "/api/config/automation/config/rolladen_az_og",
    target: "automation.rolladen_az_og",
    previewDigest: "sha256:abc123",
  };

  it("accepts valid confirmation within ttl", () => {
    const usedTokenIds = new Set<string>();
    const result = validateAndConsumeConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 60_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds,
    });

    expect(result).toEqual({ ok: true, consumedTokenId: "tok-123" });
    expect(usedTokenIds.has("tok-123")).toBe(true);
  });

  it("accepts token exactly on ttl boundary", () => {
    const result = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 10 * 60_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    expect(result).toEqual({ ok: true, consumedTokenId: "tok-123" });
  });

  it("rejects stale token", () => {
    const result = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 10 * 60_000 + 1,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    expect(result).toEqual({
      ok: false,
      reason: "stale",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });

  it("rejects replay token", () => {
    const result = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(["tok-123"]),
    });

    expect(result).toEqual({
      ok: false,
      reason: "replay",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });

  it("rejects method/path/target mismatch", () => {
    const methodMismatch = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "DELETE",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    const pathMismatch = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/anderer",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    const targetMismatch = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.falsch",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    expect(methodMismatch).toEqual({
      ok: false,
      reason: "binding_mismatch_method",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
    expect(pathMismatch).toEqual({
      ok: false,
      reason: "binding_mismatch_path",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
    expect(targetMismatch).toEqual({
      ok: false,
      reason: "binding_mismatch_target",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });

  it("rejects preview digest mismatch", () => {
    const result = validateConfirmToken("confirm:tok-123", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:wrong",
      usedTokenIds: new Set<string>(),
    });

    expect(result).toEqual({
      ok: false,
      reason: "binding_mismatch_digest",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });

  it("rejects invalid format and wrong token id", () => {
    const invalidFormat = validateConfirmToken("please apply", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    const wrongToken = validateConfirmToken("confirm:tok-xxx", baseToken, {
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      usedTokenIds: new Set<string>(),
    });

    expect(invalidFormat).toEqual({
      ok: false,
      reason: "format_invalid",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
    expect(wrongToken).toEqual({
      ok: false,
      reason: "token_mismatch",
      remediation: {
        regeneratePreview: true,
        issueFreshToken: true,
      },
    });
  });

  it("accepts natural-language confirmation for create/update when preview binding matches", () => {
    const result = validateWriteConfirmation("ja bitte erstellen", {
      tier: "create_update",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-123",
      expectedPreviewId: "pv-123",
      usedTokenIds: new Set<string>(),
    });

    expect(result).toEqual({
      ok: true,
      mode: "natural",
    });
  });

  it("rejects natural-language confirmation for create/update when preview id mismatches", () => {
    const result = validateWriteConfirmation("apply", {
      tier: "create_update",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-older",
      expectedPreviewId: "pv-current",
      usedTokenIds: new Set<string>(),
    });

    expect(result).toEqual({
      ok: false,
      reason: "preview_id_mismatch",
    });
  });

  it("requires token confirmation for destructive writes", () => {
    const naturalFail = validateWriteConfirmation("ja bitte", {
      tier: "destructive",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "DELETE",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-123",
      expectedPreviewId: "pv-123",
      usedTokenIds: new Set<string>(),
      token: baseToken,
    });
    expect(naturalFail).toEqual({
      ok: false,
      reason: "token_required",
    });

    const tokenPass = validateWriteConfirmation("confirm:tok-123", {
      tier: "destructive",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-123",
      expectedPreviewId: "pv-123",
      usedTokenIds: new Set<string>(),
      token: baseToken,
    });
    expect(tokenPass).toEqual({
      ok: true,
      mode: "token",
      consumedTokenId: "tok-123",
    });
  });

  it("accepts token confirmations with surrounding whitespace", () => {
    const createUpdate = validateWriteConfirmation("  confirm:tok-123  ", {
      tier: "create_update",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-123",
      expectedPreviewId: "pv-123",
      usedTokenIds: new Set<string>(),
      token: baseToken,
    });

    expect(createUpdate).toEqual({
      ok: true,
      mode: "token",
      consumedTokenId: "tok-123",
    });

    const destructive = validateWriteConfirmation("  confirm:tok-123  ", {
      tier: "destructive",
      nowMs: baseToken.issuedAtMs + 5_000,
      ttlMs: 10 * 60_000,
      method: "POST",
      path: "/api/config/automation/config/rolladen_az_og",
      target: "automation.rolladen_az_og",
      previewDigest: "sha256:abc123",
      previewId: "pv-123",
      expectedPreviewId: "pv-123",
      usedTokenIds: new Set<string>(),
      token: baseToken,
    });

    expect(destructive).toEqual({
      ok: true,
      mode: "token",
      consumedTokenId: "tok-123",
    });
  });
});
