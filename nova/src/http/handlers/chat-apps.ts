import type { IncomingMessage } from "node:http";

import { ChatAppsError, type ChatApps } from "../../runtime/chat-apps.js";
import { ChatPhoneError, type AdoptPhone } from "../../runtime/chat-phone-control.js";
import { checkOwner, type OwnerCheckDeps } from "../../security/owner-check.js";
import { HttpError } from "../errors.js";
import type { RouteHandler } from "../router.js";

// GET/POST /chat-apps/v1/state over ingress: the owner-only chat-app action
// the CLI calls with the owner's own ingress session (spec §11.6). Machine
// only, like /pair/v2/arm: browsers always send Sec-Fetch-Site, and JSON
// forces a CORS preflight. The NOVA page drives the same ChatApps actions.
// The owner's chat-app phone changes only through the bridge's control
// channel, and never with override (the NOVA page's click alone).

const CONNECTOR_PATH = "/api/ha_nova/mcp";
const FIELDS = new Set(["enabled", "allow_restart", "read_only", "chat_codes", "owner_phone", "owner_phone_move"]);
const DEVICE_ID = /^[0-9a-f]{32}$/;

export type ChatAppsApiDeps = OwnerCheckDeps & { chatApps: ChatApps };

async function owner(request: IncomingMessage, deps: ChatAppsApiDeps): Promise<string> {
  if (request.headers["sec-fetch-site"] !== undefined) {
    throw new HttpError(403, "MACHINE_REQUEST_REQUIRED", "Browser requests cannot change chat apps");
  }
  const result = await checkOwner(request, deps);
  if (!result.ok) {
    throw result.status === 503
      ? new HttpError(503, "OWNER_CHECK_UNAVAILABLE", result.message)
      : new HttpError(403, "OWNER_REQUIRED", result.message);
  }
  return result.userId;
}

// phone_change: this App moves and stops the chat-app phone (owner_phone_move);
// setup asks before it offers that.
function view(deps: ChatAppsApiDeps, ownerId: string, restarting = false): Record<string, unknown> {
  const s = deps.chatApps.status(ownerId);
  return {
    available: s.available,
    enabled: s.enabled,
    unfinished: s.unfinished,
    read_only: s.readOnly,
    chat_codes: s.chatCodes,
    allowed_users: s.allowedUsers,
    owner_phone: s.ownerPhone,
    owner_phone_id: s.ownerPhoneId,
    phone_change: true,
    connector_path: CONNECTOR_PATH,
    restarting,
  };
}

// GET adds the last move started here (setup polls it): the bridge's state of
// that job, or status "unread" while the bridge can't be read.
export function createChatAppsHandlers(deps: ChatAppsApiDeps): { get: RouteHandler; post: RouteHandler } {
  let lastMove: { owner: string; id: string } | null = null;
  const get: RouteHandler = async ({ request }) => {
    const ownerId = await owner(request, deps);
    const id = lastMove?.owner === ownerId && deps.chatApps.status(ownerId).enabled ? lastMove.id : null; // off: no bridge to ask
    if (id === null) {
      return view(deps, ownerId);
    }
    const job = await deps.chatApps.phone.job(id).catch(() => ({ status: "unread" }));
    return { ...view(deps, ownerId), phone_job: { ...job, id } };
  };
  const post: RouteHandler = async ({ request, body }) => {
    const contentType = request.headers["content-type"] ?? "";
    if (contentType.split(";")[0]?.trim().toLowerCase() !== "application/json") {
      throw new HttpError(415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json");
    }
    const ownerId = await owner(request, deps);
    const input = parse(body);
    try {
      // On/off first: a refusal (consent, configuration) changes nothing else.
      let restart = false;
      if (input.enabled === true) {
        restart = (await deps.chatApps.turnOn(input.allowRestart)).restart;
      } else if (input.enabled === false) {
        await deps.chatApps.turnOff();
      }
      if (input.readOnly !== undefined || input.chatCodes !== undefined) {
        deps.chatApps.setSwitches({ ...(input.readOnly !== undefined ? { readOnly: input.readOnly } : {}), ...(input.chatCodes !== undefined ? { chatCodes: input.chatCodes } : {}) });
      }
      // The owner's first chat-app phone, from a computer link their own tap made:
      // only while they have none (409 PHONE_EXISTS); a move asks the phone linked now first.
      const phone = input.ownerPhone;
      if (phone !== undefined) {
        await phoneCall(() => adopt(deps, ownerId, phone));
      }
      const move = input.ownerPhoneMove;
      const job = move === undefined ? null : await phoneCall(() => deps.chatApps.phone.start(ownerId, { action: "move", phone: { device_id: move } }));
      if (job !== null) {
        lastMove = { owner: ownerId, id: job };
      }
      const answer = { ...view(deps, ownerId, restart), ...(job === null ? {} : { phone_job: { id: job, status: "running" } }) };
      if (restart) {
        deps.chatApps.restartSoon(); // after this answer: HA carries the ingress connection
      }
      return answer;
    } catch (error) {
      if (error instanceof ChatAppsError) {
        throw new HttpError(error.status, error.code, error.message);
      }
      throw error;
    }
  };
  return { get, post };
}

// Right after a turn-on, Home Assistant may not list the phone yet (its
// Mobile App loads beside HA NOVA): the first phone is tried again a while.
async function adopt(deps: ChatAppsApiDeps, ownerId: string, phone: AdoptPhone): Promise<string | null> {
  const everyMs = 2_000;
  for (const until = Date.now() + 15_000; ; ) {
    try {
      return await deps.chatApps.phone.start(ownerId, { action: "adopt", phone });
    } catch (error) {
      if (!(error instanceof ChatPhoneError && ["PHONE_NOT_ELIGIBLE", "PHONES_UNREADABLE"].includes(error.code)) || Date.now() + everyMs > until) {
        throw error;
      }
      await new Promise((resolve) => setTimeout(resolve, everyMs));
    }
  }
}

// The bridge's refusals pass through (400, 403, 409, and its 503s: nothing
// changed; 504 CHAT_PHONE_UNKNOWN: it may have). Any other status is 503: a
// 401 or 404 would read as the owner's session or an App without chat apps.
async function phoneCall<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (error) {
    if (error instanceof ChatPhoneError) {
      throw [400, 403, 409, 503, 504].includes(error.status)
        ? new HttpError(error.status, error.code, error.message)
        : new HttpError(503, "CHAT_PHONE_UNAVAILABLE", `the chat-app bridge answered ${error.status} ${error.code}`);
    }
    throw error;
  }
}

function parse(body: unknown): { enabled?: boolean; allowRestart: boolean; readOnly?: boolean; chatCodes?: boolean; ownerPhone?: AdoptPhone; ownerPhoneMove?: string } {
  if (typeof body !== "object" || body === null || Array.isArray(body) || Object.keys(body).some((k) => !FIELDS.has(k))) {
    throw invalid("a JSON object with only enabled, allow_restart, read_only, chat_codes, owner_phone, owner_phone_move");
  }
  const b = body as Record<string, unknown>;
  const flag = (key: string): boolean | undefined => {
    if (b[key] !== undefined && typeof b[key] !== "boolean") {
      throw invalid(`${key} must be true or false`);
    }
    return b[key] as boolean | undefined;
  };
  const out: ReturnType<typeof parse> = { allowRestart: flag("allow_restart") === true };
  for (const [key, field] of [["enabled", "enabled"], ["read_only", "readOnly"], ["chat_codes", "chatCodes"]] as const) {
    const value = flag(key);
    if (value !== undefined) {
      out[field] = value;
    }
  }
  if (b.owner_phone === null) { // a stop asks that phone first: the NOVA page's Stop
    throw new HttpError(409, "OWNER_PHONE_STOP_ON_PAGE", "Stopping the chat-app phone asks that phone first. Use NOVA in Home Assistant: Chat apps, then Stop.");
  }
  if ((b.owner_phone !== undefined || b.owner_phone_move !== undefined) && Object.keys(b).length > 1) {
    throw invalid("owner_phone and owner_phone_move go alone"); // a phone refusal never leaves other changes half done
  }
  if (b.owner_phone !== undefined) {
    out.ownerPhone = phone(b.owner_phone);
  }
  if (b.owner_phone_move !== undefined) {
    const move = b.owner_phone_move as Record<string, unknown>;
    if (typeof move !== "object" || move === null || Object.keys(move).length !== 1 || typeof move.device_id !== "string" || !DEVICE_ID.test(move.device_id)) {
      throw invalid("owner_phone_move needs only device_id");
    }
    out.ownerPhoneMove = move.device_id;
  }
  return out;
}

function phone(value: unknown): AdoptPhone {
  const p = value as Record<string, unknown>;
  const ok =
    typeof value === "object" && value !== null && Object.keys(p).length === 3 &&
    typeof p.service === "string" && /^mobile_app_[a-z0-9_]{1,100}$/.test(p.service) &&
    typeof p.device_id === "string" && DEVICE_ID.test(p.device_id) &&
    typeof p.name === "string" && p.name.length > 0 && p.name.length <= 100 && !/[\u0000-\u001f\u007f]/.test(p.name);
  if (!ok) {
    throw invalid("owner_phone needs service (mobile_app_…), device_id, and name");
  }
  return { service: p.service as string, device_id: p.device_id as string, name: p.name as string };
}

function invalid(message: string): HttpError {
  return new HttpError(400, "VALIDATION_ERROR", message);
}
