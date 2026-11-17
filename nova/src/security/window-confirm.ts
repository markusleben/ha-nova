import type { DeviceRecord, DeviceRegistry, WindowRefusal } from "./device-registry-types.js";
import { isOwner, type HaAuthUser } from "./owner-check.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID, type DeviceMetadata } from "./pairing-metadata.js";
import type { Principal } from "./principal.js";

export type WindowStatus = { window: boolean; why: "on" | "off" | WindowRefusal };

export function pairingWindowFields(metadata: DeviceMetadata, owner: string | undefined, own: boolean, atMs: number): Partial<DeviceRecord> {
  return {
    ...(metadata.credentialStore !== undefined ? { credentialStore: metadata.credentialStore } : {}),
    ...(metadata.legacyCleanup !== undefined ? { legacyCleanup: metadata.legacyCleanup } : {}),
    ...(owner !== undefined ? { pairingOwnerId: owner } : {}),
    ...(owner !== undefined && metadata.retireDeviceId !== undefined ? { retireDeviceId: metadata.retireDeviceId } : {}),
    ...(owner !== undefined && own && metadata.credentialStore !== undefined && metadata.credentialStore !== "file"
      ? { ownComputer: { userId: owner, atMs }, windowConfirm: true } : {}),
  };
}

// What keeps a computer from confirming in its window whoever declares it
// theirs, or, with owner, once that owner does (null: nothing). A Mac's
// pre-0.32 sign-in must be signed out first (spec §5.3).
export function windowBlocked(record: DeviceRecord, owner?: string): WindowRefusal | null {
  if (record.state !== "active" || record.clientInstallId === BRIDGE_INSTALL_ID || record.clientInstallId === GATE_INSTALL_ID) return "device";
  if (record.credentialStore === undefined) return "pair_again";
  if (record.credentialStore === "file") return "file";
  if (record.credentialStore === "keychain" && record.legacyCleanup !== "complete") return "legacy_cleanup_pending";
  return owner !== undefined && record.cloudUserId !== undefined && record.cloudUserId !== owner ? "owner" : null;
}

export function windowStatus(principal: Principal | undefined, record: DeviceRecord | undefined): WindowStatus {
  if (principal?.kind !== "device" || principal.internal !== undefined || record === undefined || record.deviceId !== principal.deviceId) {
    return { window: false, why: "device" };
  }
  const blocked = windowBlocked(record);
  if (blocked !== null) return { window: false, why: blocked };
  if (record.ownComputer === undefined) return { window: false, why: "not_yours" };
  if (record.windowConfirm !== true) return { window: false, why: "off" };
  const other = windowBlocked(record, record.ownComputer.userId);
  return other === null ? { window: true, why: "on" } : { window: false, why: other };
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
