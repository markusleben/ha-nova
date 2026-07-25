import type { IncomingMessage } from "node:http";

const SUPERVISOR_INGRESS_PEERS = new Set([
  "172.30.32.2",
  "::ffff:172.30.32.2",
  "::ffff:ac1e:2002",
]);

const MAX_USER_ID_LENGTH = 256;
const MAX_INGRESS_PATH_LENGTH = 2_048;

export type IngressIdentity =
  { ok: true; userId: string; ingressPath: string } | { ok: false };

// Supervisor ingress identity is trusted only when both the socket peer and the
// single-value headers agree. Node joins duplicate arbitrary headers with a
// comma; rejecting commas makes duplicate/spoofed identity fail closed.
export function resolveIngressIdentity(
  request: IncomingMessage,
): IngressIdentity {
  const peer = request.socket.remoteAddress?.toLowerCase() ?? "";
  if (!SUPERVISOR_INGRESS_PEERS.has(peer)) {
    return { ok: false };
  }

  const userId = singleHeader(request, "x-remote-user-id", MAX_USER_ID_LENGTH);
  const ingressPath = singleHeader(
    request,
    "x-ingress-path",
    MAX_INGRESS_PATH_LENGTH,
  );
  if (userId === null || ingressPath === null || !ingressPath.startsWith("/")) {
    return { ok: false };
  }
  return { ok: true, userId, ingressPath };
}

function singleHeader(
  request: IncomingMessage,
  name: string,
  maxLength: number,
): string | null {
  if (headerOccurrences(request, name) !== 1) {
    return null;
  }
  const raw = request.headers[name];
  if (typeof raw !== "string" || raw.length === 0 || raw.length > maxLength) {
    return null;
  }
  if (
    raw !== raw.trim() ||
    raw.includes(",") ||
    /[\u0000-\u001f\u007f]/.test(raw)
  ) {
    return null;
  }
  return raw;
}

function headerOccurrences(request: IncomingMessage, name: string): number {
  const rawHeaders = request.rawHeaders;
  if (!Array.isArray(rawHeaders)) {
    return 0;
  }
  let count = 0;
  for (let index = 0; index < rawHeaders.length; index += 2) {
    if (rawHeaders[index]?.toLowerCase() === name) {
      count += 1;
    }
  }
  return count;
}
