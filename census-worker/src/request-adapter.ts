import {
  CensusRequestLike,
  MAX_BODY_BYTES,
  isJSONMediaType,
  readBodyCapped,
} from "./census.js";

// This is the only adapter from Cloudflare's incoming Request into the pure
// census core. scripts/test/check-census-worker-request-access.mjs pins every
// allowed read so transport metadata cannot enter application logic silently.
export async function adaptCensusRequest(
  incomingRequest: Request,
): Promise<CensusRequestLike> {
  const url = new URL(incomingRequest.url);
  const path = url.pathname;
  const localRequest =
    url.hostname === "127.0.0.1" ||
    url.hostname === "[::1]" ||
    url.hostname === "localhost";
  const contentType = incomingRequest.headers.get("content-type") ?? "";
  const declaredHeader = incomingRequest.headers.get("content-length");
  const declared = declaredHeader === null ? undefined : Number(declaredHeader);
  const accessToken =
    incomingRequest.headers.get("cf-access-jwt-assertion") ?? "";
  const localStatsToken =
    incomingRequest.headers.get("x-ha-nova-local-stats-token") ?? "";
  const contentLength =
    declared !== undefined && Number.isFinite(declared) ? declared : undefined;
  let overflow = contentLength !== undefined && contentLength > MAX_BODY_BYTES;
  let bodyText = "";
  const wantsBody =
    incomingRequest.method === "POST" &&
    (path === "/ping" || path === "/withdraw") &&
    isJSONMediaType(contentType);
  if (wantsBody && !overflow) {
    const read = await readBodyCapped(incomingRequest.body, MAX_BODY_BYTES);
    bodyText = read.text;
    overflow = read.overflow;
  }
  const requestLike: CensusRequestLike = {
    method: incomingRequest.method,
    path,
    contentType,
    bodyText,
    accessToken,
    localStatsToken,
    localRequest,
  };
  if (overflow) {
    requestLike.contentLength = MAX_BODY_BYTES + 1;
  } else if (contentLength !== undefined) {
    requestLike.contentLength = contentLength;
  }
  return requestLike;
}
