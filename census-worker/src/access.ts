import { createRemoteJWKSet, jwtVerify, type JWTVerifyGetKey } from "jose";

const keySets = new Map<string, ReturnType<typeof createRemoteJWKSet>>();

function normalizedTeamDomain(value: string): string {
  const url = new URL(value);
  if (url.protocol !== "https:") {
    throw new Error("Access team domain must use HTTPS");
  }
  return url.origin;
}

export async function verifyCloudflareAccess(
  token: string,
  teamDomain: string,
  audience: string,
  suppliedKeySet?: JWTVerifyGetKey,
): Promise<boolean> {
  if (token === "" || teamDomain === "" || audience === "") {
    return false;
  }
  try {
    const issuer = normalizedTeamDomain(teamDomain);
    let remoteKeySet = keySets.get(issuer);
    if (suppliedKeySet === undefined && remoteKeySet === undefined) {
      remoteKeySet = createRemoteJWKSet(
        new URL(`${issuer}/cdn-cgi/access/certs`),
      );
      keySets.set(issuer, remoteKeySet);
    }
    await jwtVerify(token, suppliedKeySet ?? remoteKeySet!, {
      issuer,
      audience,
    });
    return true;
  } catch {
    return false;
  }
}

export function localStatsAccess(
  presented: string,
  configured: string | undefined,
): boolean {
  return (
    configured !== undefined && configured !== "" && presented === configured
  );
}
