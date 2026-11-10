import { isLoopback, type Principal } from "../security/principal.js";
import { decodeUtf8Strict } from "../shared/utf8.js";
import { HttpError, toErrorResponse } from "./errors.js";
import type { GateRequest, PhoneAnswer, PhoneLink, PhoneText, PreparedAsk } from "./gate-asks.js";
import type { RouteContext, RouteHandler } from "./router.js";

// The safety check in front of every device-facing handler
// (docs/work/2026-11-03-relay-typed-gate-spec.md §1, §2). The Relay classifies
// nothing: the worker (`ha-nova gate`) rates the exact bytes the Relay forwards
// and redacts every answer. A failure before forwarding answers 503 and sends
// nothing; one after it withholds the answer (502).

export type GateEndpoint = "ws" | "core" | "files" | "backups" | "supervisor";

export interface GateCheck {
  verdict: "pass" | "typed" | "refuse";
  kind: "read" | "write";
  why?: string;
}

// null: no usable answer (missing, dead, slow, or malformed worker).
// consume: a grant for exactly these bytes, used up now (gate-asks.ts);
// without it no typed request is ever forwarded.
export interface GateClient {
  check(principal: Principal, endpoint: GateEndpoint, body: Buffer): Promise<GateCheck | null>;
  redact(endpoint: GateEndpoint, request: Buffer, response: Buffer): Promise<Buffer | null>;
  consume?(principal: Principal, grantId: string, endpoint: GateEndpoint, body: Buffer): boolean;
  ask?(device: string, requests: GateRequest[]): Promise<PreparedAsk | null>;
  phone?(link: PhoneLink, text: PhoneText, waitMs: number, stop: AbortSignal): Promise<PhoneAnswer | null>;
}

const APPROVAL_REQUIRED =
  "Only a person may confirm this, on their phone or in Home Assistant. Update HA NOVA on this computer: ha-nova update";

// prepare: the Relay's own parse and normalization, before the check, so what
// is rated is what is sent; the handler then runs on exactly the rated bytes.
export function gated(
  endpoint: GateEndpoint,
  handler: RouteHandler,
  gate: GateClient,
  prepare: (body: unknown) => unknown = (body) => body,
): RouteHandler {
  return async (context: RouteContext) => {
    const { principal } = context;
    if (principal === undefined) {
      throw unavailable();
    }
    const bytes = Buffer.from(JSON.stringify(prepare(context.body) ?? null));
    const send: RouteContext = { ...context, body: JSON.parse(bytes.toString("utf8")) as unknown };
    if (principal.kind === "device" && principal.internal !== undefined) {
      if (!isLoopback(principal.peer)) {
        throw refused();
      }
      if (principal.internal === "chat-bridge") {
        return await handler(send); // keeps its own asks and redaction (§5)
      }
      if (!phoneAsk(endpoint, send.body)) {
        const check = await gate.check(principal, endpoint, bytes);
        if (check === null) {
          throw unavailable();
        }
        if (check.verdict !== "pass" || check.kind !== "read") {
          throw refused(check.why);
        }
      }
      await stillAuthorized(context);
      return await handler(send); // the worker needs the action ids it sent: unredacted
    }

    const check = await gate.check(principal, endpoint, bytes);
    if (check === null) {
      throw unavailable();
    }
    if (check.verdict === "refuse") {
      throw refused(check.why);
    }
    if (check.verdict === "typed") {
      const grant = context.request.headers["x-ha-nova-grant"];
      if (typeof grant !== "string" || gate.consume?.(principal, grant, endpoint, bytes) !== true) {
        throw new HttpError(428, "GATE_APPROVAL_REQUIRED", APPROVAL_REQUIRED);
      }
    }
    await stillAuthorized(context);

    let status = 200;
    let envelope: unknown;
    try {
      const data = await handler(send);
      if (endpoint === "core" && (data as { body_encoding?: unknown } | null)?.body_encoding === "base64") {
        return data; // a binary answer (camera frame): no JSON or text to redact
      }
      envelope = { ok: true, data: data ?? null };
    } catch (error) {
      if (!(error instanceof HttpError)) {
        throw error; // answered as a bare 500
      }
      ({ status, body: envelope } = toErrorResponse(error));
    }
    const redacted = await gate.redact(endpoint, bytes, Buffer.from(JSON.stringify(envelope)));
    if (redacted === null || !isJson(redacted)) {
      throw new HttpError(502, "GATE_ANSWER_WITHHELD", "Home Assistant may have carried this out; HA NOVA withheld the answer");
    }
    context.response.statusCode = status;
    context.response.setHeader("content-type", "application/json; charset=utf-8");
    context.response.setHeader("content-length", redacted.byteLength);
    context.response.end(redacted);
    return undefined;
  };
}

// A credential revoked while the worker checked (up to 20 s) sends nothing.
async function stillAuthorized(context: RouteContext): Promise<void> {
  if (context.reauthorize !== undefined && !(await context.reauthorize())) {
    throw new HttpError(401, "UNAUTHORIZED", "This device's access ended while HA NOVA checked the request; nothing was sent");
  }
}

// The worker's own ask (§2, gate principal only): a notification to a phone,
// sent as the CLI sends every service call, and collecting the phone's answer.
const PHONE_NOTIFY = /^\/api\/services\/notify\/mobile_app_[a-z0-9_]+$/;

function phoneAsk(endpoint: GateEndpoint, body: unknown): boolean {
  if (!isObject(body)) {
    return false;
  }
  if (endpoint === "core") {
    return (
      Object.keys(body).length === 3 &&
      body.method === "POST" &&
      typeof body.path === "string" &&
      PHONE_NOTIFY.test(body.path) &&
      isObject(body.body)
    );
  }
  const message = body.message;
  return (
    endpoint === "ws" &&
    !("type" in body) &&
    isObject(body.collect_events) &&
    isObject(message) &&
    Object.keys(message).length === 2 &&
    message.type === "subscribe_events" &&
    message.event_type === "mobile_app_notification_action"
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isJson(bytes: Buffer): boolean {
  try {
    JSON.parse(decodeUtf8Strict(bytes));
    return true;
  } catch {
    return false;
  }
}

function unavailable(): HttpError {
  return new HttpError(503, "GATE_UNAVAILABLE", "HA NOVA's safety check is not available, so nothing was sent. Try again in a minute; if it stays, restart the NOVA App.");
}

// why: the worker's reason id, nothing else. 422: a released CLI reads 403 as a bad credential.
function refused(why?: string): HttpError {
  const reason = why !== undefined && /^[a-z0-9_.-]{1,64}$/.test(why) ? ` (${why})` : "";
  return new HttpError(422, "GATE_REFUSED", `HA NOVA's safety check refused this request${reason}; nothing was sent.`);
}
