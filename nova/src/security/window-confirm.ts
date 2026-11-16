import type { DeviceRecord, DeviceRegistry } from "./device-registry-types.js";
import { isOwner, type HaAuthUser } from "./owner-check.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID, type DeviceMetadata } from "./pairing-metadata.js";
import type { Principal } from "./principal.js";

export type WindowStatus = { window: boolean; why: "on" | "device" | "pair_again" | "file" | "not_yours" | "off" | "owner" };

export function pairingWindowFields(metadata: DeviceMetadata, owner: string | undefined, own: boolean, atMs: number): Partial<DeviceRecord> {
  return {
    ...(metadata.credentialStore !== undefined ? { credentialStore: metadata.credentialStore } : {}),
    ...(owner !== undefined ? { pairingOwnerId: owner } : {}),
    ...(owner !== undefined && own && metadata.credentialStore !== undefined && metadata.credentialStore !== "file"
      ? { ownComputer: { userId: owner, atMs }, windowConfirm: true } : {}),
  };
}

export function windowStatus(principal: Principal | undefined, record: DeviceRecord | undefined): WindowStatus {
  if (principal?.kind !== "device" || principal.internal !== undefined || record === undefined || record.state !== "active" || record.deviceId !== principal.deviceId || record.clientInstallId === BRIDGE_INSTALL_ID || record.clientInstallId === GATE_INSTALL_ID) {
    return { window: false, why: "device" };
  }
  if (record.credentialStore === undefined) {
    return { window: false, why: "pair_again" };
  }
  if (record.credentialStore === "file") {
    return { window: false, why: "file" };
  }
  if (record.ownComputer === undefined) {
    return { window: false, why: "not_yours" };
  }
  if (record.windowConfirm !== true) {
    return { window: false, why: "off" };
  }
  if (record.cloudUserId !== undefined && record.cloudUserId !== record.ownComputer.userId) {
    return { window: false, why: "owner" };
  }
  return { window: true, why: "on" };
}

export async function checkWindowConfirm(principal: Principal | undefined, registry: Pick<DeviceRegistry, "list">, fetchAuthUsers: () => Promise<unknown>): Promise<WindowStatus> {
  const read = () => principal?.kind === "device" ? registry.list().find((d) => d.deviceId === principal.deviceId) : undefined;
  const before = read();
  const status = windowStatus(principal, before);
  if (!status.window) {
    return status;
  }
  const users = await fetchAuthUsers();
  if (!Array.isArray(users)) {
    throw new Error("config/auth/list returned a non-list result");
  }
  const after = read();
  const current = windowStatus(principal, after);
  if (!current.window) {
    return current;
  }
  const owner = users.find((user: HaAuthUser) => user.id === after!.ownComputer!.userId);
  return owner !== undefined && isOwner(owner) ? current : { window: false, why: "owner" };
}
