import {
  CensusRequestLike,
  MAX_BODY_BYTES,
  isJSONMediaType,
  readBodyCapped,
} from "./census";

// This is the only adapter from Cloudflare's incoming Request into the pure
// census core. scripts/test/check-census-worker-request-access.mjs pins every
// allowed read so transport metadata cannot enter application logic silently.
export async function adaptCensusRequest(
  incomingRequest: Request,
): Promise<CensusRequestLike> {
  const path = new URL(incomingRequest.url).pathname;
  const contentType = incomingRequest.headers.get("content-type") ?? "";
  const declared = Number(incomingRequest.headers.get("content-length") ?? "");
  const contentLength = Number.isFinite(declared) ? declared : undefined;
  let overflow = contentLength !== undefined && contentLength > MAX_BODY_BYTES;
  let bodyText = "";
  const wantsBody =
    incomingRequest.method === "POST" &&
    path === "/ping" &&
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
  };
  if (overflow) {
    requestLike.contentLength = MAX_BODY_BYTES + 1;
  } else if (contentLength !== undefined) {
    requestLike.contentLength = contentLength;
  }
  return requestLike;
}
