import type { IncomingMessage, ServerResponse } from "node:http";

import { checkOwner, type HaAuthUser } from "../../security/owner-check.js";
import type { CsrfStore } from "../../security/csrf.js";
import type { DeviceRegistry } from "../../security/device-registry.js";
import type { PairingV1Manager } from "../../security/pairing-v1.js";
import type { RouteHandler } from "../router.js";
import {
  escapeHtml,
  parseConfirm,
  renderPage,
  type ConnectionStatus,
  type NovaAction,
  type UpdateStatus,
} from "./nova-page-view.js";

// The NOVA owner console, served over Supervisor ingress. Server-rendered HTML
// with NO JavaScript and no external resources; every mutating action is a POST
// form guarded by a single-use CSRF token, Sec-Fetch/Origin checks, and a fresh
// owner re-check. Post/Redirect/Get avoids re-submission. The visible name is
// only "NOVA"; the code is shown solely while a pairing is active and is never
// logged. Rendering lives in nova-page-view.ts; this module owns the HTTP,
// action, and CSRF handling.

export type { ConnectionStatus, NovaAction, UpdateStatus } from "./nova-page-view.js";

const CSP = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "img-src 'self' data:",
  "base-uri 'none'",
  "form-action 'self'",
  "frame-ancestors 'self'",
].join("; ");

const ACTIONS = new Set<NovaAction>(["generate_code", "cancel_code", "revoke_device", "revoke_legacy", "reset_registry"]);

export interface NovaPageDeps {
  fetchAuthUsers: () => Promise<HaAuthUser[]>;
  csrf: CsrfStore;
  pairing: PairingV1Manager;
  registry: DeviceRegistry;
  // Corrupt-registry recovery. Defaulted for standalone/tests where a corrupt
  // registry cannot occur (single-listener path has no registry).
  registryCorrupt?: () => boolean;
  resetRegistry?: () => void;
  connection: () => ConnectionStatus;
  update: () => Promise<UpdateStatus>;
  relayVersion: string;
  now: () => number;
}

function setHeaders(response: ServerResponse): void {
  response.setHeader("content-security-policy", CSP);
  response.setHeader("cache-control", "no-store");
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("x-frame-options", "SAMEORIGIN");
  response.setHeader("referrer-policy", "no-referrer");
}

function fail(response: ServerResponse, status: number, message: string): void {
  setHeaders(response);
  response.statusCode = status;
  response.setHeader("content-type", "text/html; charset=utf-8");
  response.end(`<!doctype html><meta charset="utf-8"><title>NOVA</title><p>${escapeHtml(message)}</p>`);
}

export function createNovaPageHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers });
    if (!owner.ok) {
      fail(response, owner.status, owner.status === 503 ? "Could not verify owner access. Try again shortly." : "Owner access required.");
      return;
    }
    const [update] = await Promise.all([safeUpdate(deps)]);
    const query = new URLSearchParams((request.url ?? "").split("?")[1] ?? "");
    const html = renderPage({
      ownerName: owner.name,
      csrf: deps.csrf,
      userId: owner.userId,
      now: deps.now(),
      pairing: deps.pairing.getStatus(),
      devices: deps.registry.list(),
      hasLegacy: deps.registry.hasLegacy(),
      registryCorrupt: deps.registryCorrupt?.() ?? false,
      connection: deps.connection(),
      update,
      relayVersion: deps.relayVersion,
      ingressPath: singleHeader(request, "x-ingress-path") ?? "/home",
      // The action handler redirects here with ?err=<code> when an owner action
      // threw ("1") or the typed reset confirmation did not match ("confirm").
      errorCode: query.get("err"),
      confirm: parseConfirm(query),
    });
    setHeaders(response);
    response.statusCode = 200;
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.end(html);
  };
}

export function createNovaActionHandler(deps: NovaPageDeps): RouteHandler {
  return async ({ request, response, body }) => {
    const owner = await checkOwner(request, { fetchAuthUsers: deps.fetchAuthUsers });
    if (!owner.ok) {
      fail(response, owner.status, "Owner access required.");
      return;
    }
    // Reject cross-site form submissions. Sec-Fetch-Site, when the browser sends
    // it, must be same-origin/same-site/none; an explicit cross-site is refused.
    const fetchSite = singleHeader(request, "sec-fetch-site");
    if (fetchSite !== null && fetchSite !== "same-origin" && fetchSite !== "same-site" && fetchSite !== "none") {
      fail(response, 403, "Cross-site request refused.");
      return;
    }
    const form = (body ?? {}) as Record<string, unknown>;
    const action = typeof form.action === "string" ? form.action : "";
    const token = typeof form.csrf === "string" ? form.csrf : "";
    if (!ACTIONS.has(action as NovaAction)) {
      fail(response, 400, "Unknown action.");
      return;
    }
    // Destructive actions are two-step: their CSRF token is issued ONLY by the
    // confirm screen, and the revoke token is bound to the exact device id — a
    // token armed for one device cannot revoke another, and no token for these
    // actions exists outside an open confirm screen.
    const deviceId = typeof form.device_id === "string" ? form.device_id : "";
    const csrfAction =
      action === "revoke_device" ? `revoke_device:${deviceId}` : action;
    if (!deps.csrf.consume(owner.userId, csrfAction, token, deps.now())) {
      fail(response, 403, "This form expired. Reload the page and try again.");
      return;
    }

    let errCode: string | null = null;
    // The strongest gate: resetting the registry additionally requires the
    // owner to type RESET verbatim on the confirm screen. The consumed token
    // stays consumed on a mismatch — re-arming means opening the screen again.
    if (action === "reset_registry" && form.confirm_text !== "RESET") {
      errCode = "confirm";
    } else {
      try {
        applyAction(deps, action as NovaAction, form);
      } catch {
        // Do not leak internals, but tell the owner it failed via ?err=1 below
        // so a failed "Connect a device" is not a silent reload with no code.
        errCode = "1";
      }
    }

    // Post/Redirect/Get back to the page. The redirect target MUST keep the
    // ingress base path's trailing slash: Home Assistant serves the console at
    // "<ingress-path>/" (forwarding GET /) but 404s the slash-less base path.
    const ingressPath = singleHeader(request, "x-ingress-path") ?? "";
    const base = `${ingressPath.replace(/\/$/, "")}/`;
    setHeaders(response);
    response.statusCode = 303;
    // A RESET mismatch redirects back INTO the armed confirm screen (fresh
    // token) so the error's "type RESET" instruction has an input to point at.
    const location =
      errCode === null
        ? base
        : errCode === "confirm"
          ? `${base}?err=confirm&confirm=reset_registry`
          : `${base}?err=${errCode}`;
    response.setHeader("location", location);
    response.end();
  };
}

function applyAction(deps: NovaPageDeps, action: NovaAction, form: Record<string, unknown>): void {
  switch (action) {
    case "generate_code":
      deps.pairing.generateCode();
      return;
    case "cancel_code":
      deps.pairing.cancel();
      return;
    case "revoke_device": {
      const deviceId = typeof form.device_id === "string" ? form.device_id : "";
      if (deviceId) {
        deps.registry.revoke(deviceId);
      }
      return;
    }
    case "revoke_legacy":
      // Cut off the migrated shared token so a pre-pairing credential cannot
      // outlive the devices; paired devices keep working.
      deps.registry.revokeLegacy();
      return;
    case "reset_registry":
      // Recover from a corrupt registry: archive the damaged file and start a
      // fresh, empty one so pairing works again.
      deps.resetRegistry?.();
      return;
  }
}

async function safeUpdate(deps: NovaPageDeps): Promise<UpdateStatus> {
  try {
    return await deps.update();
  } catch {
    return { version: deps.relayVersion, versionLatest: null, updateAvailable: false, error: true };
  }
}

function singleHeader(request: IncomingMessage, name: string): string | null {
  const value = request.headers[name];
  return typeof value === "string" ? value : null;
}
