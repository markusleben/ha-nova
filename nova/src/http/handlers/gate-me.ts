import type { DeviceRegistry } from "../../security/device-registry-types.js";
import { checkWindowConfirm, codeOwner, codeState } from "../../security/window-confirm.js";
import { HttpError } from "../errors.js";
import type { RouteHandler } from "../router.js";

export function createGateMeHandler(registry: Pick<DeviceRegistry, "list">, fetchAuthUsers: () => Promise<unknown>): RouteHandler {
  return async ({ principal, response }) => {
    response.setHeader("cache-control", "no-store");
    try {
      const status = await checkWindowConfirm(principal, registry, fetchAuthUsers);
      const record = principal?.kind === "device" ? registry.list().find((d) => d.deviceId === principal.deviceId) : undefined;
      const code = codeState(principal, record); // MAR-244: phone, a link is stored; code "owner": the owner couldn't be read
      const mode = code.mode !== "on" ? code.mode : await codeOwner(code.userId, fetchAuthUsers).then((ok) => (ok ? "on" : "off"), () => "owner");
      return { ...status, phone: record?.state === "active" && record.confirmPhone !== undefined, code: mode };
    } catch {
      throw new HttpError(503, "GATE_UNAVAILABLE", "The Home Assistant owner could not be checked; this computer cannot confirm now");
    }
  };
}
