import type { IncomingMessage } from "node:http";

import { ChatAppsError, type ChatApps, type PhoneLinkInput } from "../../runtime/chat-apps.js";
import { checkOwner, type OwnerCheckDeps } from "../../security/owner-check.js";
import { HttpError } from "../errors.js";
import type { RouteHandler } from "../router.js";

// GET/POST /chat-apps/v1/state over ingress: the owner-only chat-app action
// the CLI calls with the owner's own ingress session (spec §11.6). Machine
// only, like /pair/v2/arm: browsers always send Sec-Fetch-Site, and JSON
// forces a CORS preflight. The NOVA page drives the same ChatApps actions.

const CONNECTOR_PATH = "/api/ha_nova/mcp";
const FIELDS = new Set(["enabled", "allow_restart", "read_only", "chat_codes", "owner_phone"]);

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
    connector_path: CONNECTOR_PATH,
    restarting,
  };
}

export function createChatAppsGetHandler(deps: ChatAppsApiDeps): RouteHandler {
  return async ({ request }) => view(deps, await owner(request, deps));
}

export function createChatAppsPostHandler(deps: ChatAppsApiDeps): RouteHandler {
  return async ({ request, body }) => {
    const contentType = request.headers["content-type"] ?? "";
    if (contentType.split(";")[0]?.trim().toLowerCase() !== "application/json") {
      throw new HttpError(415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json");
    }
    const ownerId = await owner(request, deps);
    const input = parse(body);
    try {
      if (input.readOnly !== undefined || input.chatCodes !== undefined) {
        deps.chatApps.setSwitches({ ...(input.readOnly !== undefined ? { readOnly: input.readOnly } : {}), ...(input.chatCodes !== undefined ? { chatCodes: input.chatCodes } : {}) });
      }
      if (input.ownerPhone !== undefined) {
        deps.chatApps.setOwnerPhone(ownerId, input.ownerPhone);
      }
      let restart = false;
      if (input.enabled === true) {
        restart = (await deps.chatApps.turnOn(input.allowRestart)).restart;
      } else if (input.enabled === false) {
        await deps.chatApps.turnOff();
      }
      const answer = view(deps, ownerId, restart);
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
}

function parse(body: unknown): { enabled?: boolean; allowRestart: boolean; readOnly?: boolean; chatCodes?: boolean; ownerPhone?: PhoneLinkInput | null } {
  if (typeof body !== "object" || body === null || Array.isArray(body) || Object.keys(body).some((k) => !FIELDS.has(k))) {
    throw invalid("a JSON object with only enabled, allow_restart, read_only, chat_codes, owner_phone");
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
  if (b.owner_phone !== undefined) {
    out.ownerPhone = b.owner_phone === null ? null : phone(b.owner_phone);
  }
  return out;
}

function phone(value: unknown): PhoneLinkInput {
  const p = value as Record<string, unknown>;
  const ok =
    typeof value === "object" && value !== null && Object.keys(p).length === 3 &&
    typeof p.service === "string" && /^mobile_app_[a-z0-9_]{1,100}$/.test(p.service) &&
    typeof p.device_id === "string" && /^[0-9a-f]{32}$/.test(p.device_id) &&
    typeof p.name === "string" && p.name.length > 0 && p.name.length <= 100 && !/[\u0000-\u001f\u007f]/.test(p.name);
  if (!ok) {
    throw invalid("owner_phone needs service (mobile_app_…), device_id, and name");
  }
  return { service: p.service as string, device_id: p.device_id as string, name: p.name as string };
}

function invalid(message: string): HttpError {
  return new HttpError(400, "VALIDATION_ERROR", message);
}
