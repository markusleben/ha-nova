export type UpstreamTokenSource = "supervisor_token" | "env_ha_llat" | "none";

export type UpstreamCapability = "full" | "none";

export interface UpstreamTokenResolution {
  token: string | null;
  source: UpstreamTokenSource;
  capability: UpstreamCapability;
  warnings: string[];
}

export interface ResolveUpstreamTokenInput {
  supervisorToken?: string;
  envHaLlat?: string;
}

const MISSING_TOKEN_WARNING =
  "No upstream token available. Configure SUPERVISOR_TOKEN for the App or HA_LLAT for standalone use.";

export function resolveUpstreamToken(
  input: ResolveUpstreamTokenInput
): UpstreamTokenResolution {
  const supervisorToken = normalizeToken(input.supervisorToken);
  if (supervisorToken) {
    return success(supervisorToken, "supervisor_token", "full");
  }

  const envHaLlat = normalizeToken(input.envHaLlat);
  if (envHaLlat) {
    return success(envHaLlat, "env_ha_llat", "full");
  }

  return {
    token: null,
    source: "none",
    capability: "none",
    warnings: [MISSING_TOKEN_WARNING]
  };
}

function success(
  token: string,
  source: UpstreamTokenSource,
  capability: UpstreamCapability,
  warnings: string[] = []
): UpstreamTokenResolution {
  return {
    token,
    source,
    capability,
    warnings
  };
}

function normalizeToken(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  if (!trimmed) {
    return undefined;
  }

  return trimmed;
}
