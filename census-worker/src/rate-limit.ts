import {
  CensusRequestLike,
  MAX_BODY_BYTES,
  hashInstallationID,
  isJSONMediaType,
  validatePing,
  validateWithdraw,
} from "./census.js";

export interface MutationRateIdentity {
  route: "ping" | "withdraw";
  key: string;
}

// Invalid traffic never consumes a valid mutation's quota. Schema-2 limits
// are isolated by the same pseudonymous ID hash used for storage; withdrawal
// also has a separate binding, so report traffic cannot block deletion.
export async function mutationRateIdentity(
  request: CensusRequestLike,
): Promise<MutationRateIdentity | undefined> {
  if (
    request.method !== "POST" ||
    !isJSONMediaType(request.contentType) ||
    (request.contentLength !== undefined &&
      request.contentLength > MAX_BODY_BYTES)
  ) {
    return undefined;
  }
  if (request.path === "/withdraw") {
    const validation = validateWithdraw(request.bodyText);
    if (!validation.ok) return undefined;
    return {
      route: "withdraw",
      key: await hashInstallationID(validation.withdraw.installation_id),
    };
  }
  if (request.path !== "/ping") return undefined;
  const validation = validatePing(request.bodyText);
  if (!validation.ok) return undefined;
  if (validation.ping.schema === 1) {
    return undefined;
  }
  return {
    route: "ping",
    key: await hashInstallationID(validation.ping.installation_id),
  };
}
