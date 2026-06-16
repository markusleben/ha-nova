import { HaWsClientError, type HaWsRequest } from "../../ha/ws-client.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

export interface WsProxyHandlerOptions {
  wsClient: {
    sendMessage(message: HaWsRequest): Promise<unknown>;
  };
}

export function createWsProxyHandler(options: WsProxyHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const request = parseWsRequestBody(body);

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

  // The relay is request/response only. Subscription and live-update commands
  // resolve only on their initial ack and then emit events the relay cannot
  // deliver, forcing the HA client library to auto-unsubscribe — useless over
  // request/response. Reject them at the boundary so a client can't churn or
  // accumulate upstream subscriptions.
  if (isSubscriptionWsType(type)) {
    throw new HttpError(
      400,
      "UNSUPPORTED_WS_TYPE",
      `WS type '${type.trim()}' is a subscription or live-update command; the relay supports request/response commands only`,
    );
  }

  return body as HaWsRequest;
}

function isSubscriptionWsType(type: string): boolean {
  const t = type.trim();
  return t.startsWith("subscribe_") || t === "render_template";
}
