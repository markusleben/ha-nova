import type { IncomingMessage, ServerResponse } from "node:http";
import { describe, expect, it, vi } from "vitest";
import { createGateMeHandler } from "../../nova/src/http/handlers/gate-me.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry-types.js";

const record: DeviceRecord = {
  deviceId: "computer", secretDigest: "digest", state: "active", clientInstallId: "install",
  name: "Computer", platform: "darwin", client: "cli", createdAtMs: 1,
  credentialStore: "keychain", legacyCleanup: "complete", ownComputer: { userId: "owner", atMs: 1 }, windowConfirm: true,
};
const context = () => ({
  request: {} as IncomingMessage,
  response: { setHeader: vi.fn() } as unknown as ServerResponse,
  path: "/gate/v1/me", body: undefined,
  principal: { kind: "device" as const, deviceId: "computer" },
});

describe("this computer's window status", () => {
  it("returns only status and never caches it", async () => {
    const c = context();
    const handler = createGateMeHandler({ list: () => [record] }, async () => [{ id: "owner", is_owner: true, is_active: true }]);
    expect(await handler(c)).toEqual({ window: true, why: "on" });
    expect(c.response.setHeader).toHaveBeenCalledWith("cache-control", "no-store");
  });

  it("turns an unavailable or malformed owner read into a closed 503", async () => {
    const users = vi.fn().mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({});
    const handler = createGateMeHandler({ list: () => [record] }, users);
    await expect(handler(context())).rejects.toMatchObject({ status: 503, code: "GATE_UNAVAILABLE" });
    await expect(handler(context())).rejects.toMatchObject({ status: 503, code: "GATE_UNAVAILABLE" });
  });

  it("gives a shared principal no window and no owner lookup", async () => {
    const users = vi.fn();
    const handler = createGateMeHandler({ list: () => [record] }, users);
    expect(await handler({ ...context(), principal: { kind: "shared" } })).toEqual({ window: false, why: "device" });
    expect(users).not.toHaveBeenCalled();
  });
});
