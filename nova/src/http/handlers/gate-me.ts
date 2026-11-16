import type { DeviceRegistry } from "../../security/device-registry-types.js";
import { checkWindowConfirm } from "../../security/window-confirm.js";
import { HttpError } from "../errors.js";
import type { RouteHandler } from "../router.js";

export function createGateMeHandler(registry: Pick<DeviceRegistry, "list">, fetchAuthUsers: () => Promise<unknown>): RouteHandler {
  return async ({ principal, response }) => {
    response.setHeader("cache-control", "no-store");
    try {
      return await checkWindowConfirm(principal, registry, fetchAuthUsers);
    } catch {
      throw new HttpError(503, "GATE_UNAVAILABLE", "The Home Assistant owner could not be checked; this computer cannot confirm now");
    }
  };
}
