export type UpstreamTokenSource =
  | "env_ha_llat"
  | "app_option_ha_llat"
  | "supervisor_token"
  | "none";

export type UpstreamCapability = "full" | "limited" | "none";

export interface UpstreamTokenResolution {
  token: string | null;
  source: UpstreamTokenSource;
  capability: UpstreamCapability;
  warnings: string[];
}

export interface ResolveUpstreamTokenInput {
  envHaLlat?: string;
  appOptionHaLlat?: string;
  supervisorToken?: string;
}

const SUPERVISOR_WARNING =
  "LLAT missing. Falling back to SUPERVISOR_TOKEN with limited WebSocket scope.";
const MISSING_TOKEN_WARNING =
  "No upstream token available. Configure HA_LLAT or app option 'ha_llat'.";

export function resolveUpstreamToken(
  input: ResolveUpstreamTokenInput
): UpstreamTokenResolution {
  const envHaLlat = normalizeToken(input.envHaLlat);
  if (envHaLlat) {
    return success(envHaLlat, "env_ha_llat", "full");
  }

  const appOptionHaLlat = normalizeToken(input.appOptionHaLlat);
  if (appOptionHaLlat) {
    return success(appOptionHaLlat, "app_option_ha_llat", "full");
  }

  const supervisorToken = normalizeToken(input.supervisorToken);
  if (supervisorToken) {
    return success(supervisorToken, "supervisor_token", "limited", [SUPERVISOR_WARNING]);
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
