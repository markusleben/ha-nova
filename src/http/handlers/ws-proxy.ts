import { HaWsClientError, type HaWsRequest } from "../../ha/ws-client.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import { WsAllowlistError, type WsAllowlist } from "../../security/ws-allowlist.js";

export interface WsProxyHandlerOptions {
  wsClient: {
    sendMessage(message: HaWsRequest): Promise<unknown>;
  };
  allowlist: WsAllowlist;
}

export function createWsProxyHandler(options: WsProxyHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const request = parseWsRequestBody(body);

    try {
      options.allowlist.assertAllowed(request.type);
    } catch (error) {
      if (error instanceof WsAllowlistError) {
        throw new HttpError(403, error.code, error.message);
      }
      throw error;
    }

    try {
      return await options.wsClient.sendMessage(request);
    } catch (error) {
      if (error instanceof HaWsClientError) {
        throw new HttpError(502, error.code, error.message);
      }

      const message = error instanceof Error && error.message ? error.message : "WS upstream request failed";
      throw new HttpError(502, "UPSTREAM_WS_ERROR", message);
    }
  };
}

function parseWsRequestBody(body: unknown): HaWsRequest {
  if (!body || typeof body !== "object") {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must contain a string field 'type'");
  }

  const type = (body as { type?: unknown }).type;
  if (typeof type !== "string" || type.trim().length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must contain a string field 'type'");
  }

  return body as HaWsRequest;
}
