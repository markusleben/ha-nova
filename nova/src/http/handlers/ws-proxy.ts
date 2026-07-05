import { HaWsClientError, type HaWsEventCollectionOptions, type HaWsRequest } from "../../ha/ws-client.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

export interface WsProxyHandlerOptions {
  wsClient: {
    sendMessage(message: HaWsRequest): Promise<unknown>;
    collectMessageEvents?(
      message: HaWsRequest,
      options?: HaWsEventCollectionOptions
    ): Promise<unknown[]>;
  };
}

interface ParsedWsProxyRequest {
  message: HaWsRequest;
  collectEvents?: HaWsEventCollectionOptions;
}

const MAX_COLLECT_EVENTS = 100;
const MAX_COLLECT_TIMEOUT_MS = 10_000;

export function createWsProxyHandler(options: WsProxyHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const request = parseWsRequestBody(body);

    try {
      if (request.collectEvents) {
        const events = await collectFiniteEvents(
          options.wsClient,
          request.message,
          request.collectEvents
        );
        return { events };
      }

      return await options.wsClient.sendMessage(request.message);
    } catch (error) {
      if (error instanceof HaWsClientError) {
        throw new HttpError(502, error.code, error.message);
      }

      const message = error instanceof Error && error.message ? error.message : "WS upstream request failed";
      throw new HttpError(502, "UPSTREAM_WS_ERROR", message);
    }
  };
}

function parseWsRequestBody(body: unknown): ParsedWsProxyRequest {
  if (!body || typeof body !== "object") {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must contain a string field 'type'");
  }

  const raw = body as Record<string, unknown>;
  if (isEventCollectionEnvelope(raw)) {
    return parseEnvelopeRequest(raw);
  }

  const message = parseHaWsMessage(body);
  rejectUnsupportedWsType(message.type);
  return { message };
}

function isEventCollectionEnvelope(body: Record<string, unknown>): boolean {
  return !("type" in body) &&
    "collect_events" in body &&
    isWsMessageLike(body.message);
}

function isWsMessageLike(value: unknown): boolean {
  return !!value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    typeof (value as { type?: unknown }).type === "string" &&
    (value as { type: string }).type.trim().length > 0;
}

function parseEnvelopeRequest(body: Record<string, unknown>): ParsedWsProxyRequest {
  const message = parseHaWsMessage(body.message);
  rejectUnsupportedWsType(message.type);

  if (!("collect_events" in body)) {
    return { message };
  }

  return {
    message,
    collectEvents: parseCollectEventsOptions(body.collect_events),
  };
}

function parseHaWsMessage(value: unknown): HaWsRequest {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must contain a string field 'type'");
  }

  const type = (value as { type?: unknown }).type;
  if (typeof type !== "string" || type.trim().length === 0) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must contain a string field 'type'");
  }

  return value as HaWsRequest;
}

function rejectUnsupportedWsType(type: string): void {
  // The relay is request/response only. Infinite subscription and live-update
  // commands resolve only on their initial ack and then emit events the relay
  // cannot deliver. Reject them at the boundary so a client can't churn or
  // accumulate upstream subscriptions.
  if (isSubscriptionWsType(type)) {
    throw new HttpError(
      400,
      "UNSUPPORTED_WS_TYPE",
      `WS type '${type.trim()}' is a subscription or live-update command; the relay supports request/response commands only`,
    );
  }
}

function parseCollectEventsOptions(value: unknown): HaWsEventCollectionOptions {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "collect_events must be an object"
    );
  }

  const raw = value as Record<string, unknown>;
  const options: HaWsEventCollectionOptions = {};

  if ("until_type" in raw) {
    if (
      typeof raw.until_type !== "string" ||
      raw.until_type.trim().length === 0 ||
      raw.until_type.length > 128
    ) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "collect_events.until_type must be a non-empty string"
      );
    }
    options.finishEventType = raw.until_type;
  }

  if ("max_events" in raw) {
    const maxEvents = raw.max_events;
    if (
      typeof maxEvents !== "number" ||
      !Number.isInteger(maxEvents) ||
      maxEvents < 1 ||
      maxEvents > MAX_COLLECT_EVENTS
    ) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        `collect_events.max_events must be an integer from 1 to ${MAX_COLLECT_EVENTS}`
      );
    }
    options.maxEvents = maxEvents;
  }

  if ("timeout_ms" in raw) {
    const timeoutMs = raw.timeout_ms;
    if (
      typeof timeoutMs !== "number" ||
      !Number.isInteger(timeoutMs) ||
      timeoutMs < 1 ||
      timeoutMs > MAX_COLLECT_TIMEOUT_MS
    ) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        `collect_events.timeout_ms must be an integer from 1 to ${MAX_COLLECT_TIMEOUT_MS}`
      );
    }
    options.timeoutMs = timeoutMs;
  }

  return options;
}

async function collectFiniteEvents(
  wsClient: WsProxyHandlerOptions["wsClient"],
  request: HaWsRequest,
  collectionOptions: HaWsEventCollectionOptions
): Promise<unknown[]> {
  if (!wsClient.collectMessageEvents) {
    throw new HttpError(
      502,
      "UPSTREAM_WS_ERROR",
      "WS event collection is not supported by this relay"
    );
  }

  return await wsClient.collectMessageEvents(request, collectionOptions);
}

function isSubscriptionWsType(type: string): boolean {
  const t = type.trim().toLowerCase();
  // These open an upstream subscription this request/response relay can't deliver,
  // so a client could silently accumulate upstream churn. Block the classic
  // `subscribe_*` commands, any slash-namespaced `.../subscribe[/...]` command
  // (e.g. config_entries/subscribe, config_entries/flow/subscribe), and the
  // streaming `render_template`.
  return (
    t === "render_template" ||
    t.startsWith("subscribe_") ||
    t.endsWith("/subscribe") ||
    t.includes("/subscribe/")
  );
}
