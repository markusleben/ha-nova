import type { DeviceRegistry } from "../security/device-registry.js";

// A no-op registry used only when the on-disk registry is corrupt: every read
// is empty and every mutation throws, so device auth and pairing fail closed
// while the owner page stays reachable to trigger a reset.
export function openInertRegistry(): DeviceRegistry {
  const nope = (): never => {
    throw new Error("device registry is corrupt");
  };
  return {
    list: () => [],
    resolveDeviceSecret: () => null,
    resolveCloudDeviceSecret: () => null,
    getPairingResponse: () => null,
    createPending: nope,
    createPendingWithResponse: nope,
    activate: nope,
    activatePending: () => null,
    bindCloudUser: () => ({ ok: false, reason: "unknown" }),
    activatePendingForCloud: () => ({ ok: false, reason: "unknown" }),
    revokeCloudDevice: () => ({ ok: false, reason: "unknown" }),
    revoke: () => false,
    setConfirmPhone: () => false,
    disableWindowConfirm: () => false,
    setOwnComputer: () => "device",
    setWindowConfirm: () => "device",
    setCodeConfirm: () => "device",
    completeLegacyCleanup: () => false,
  };
}

// A stable DeviceRegistry facade that always delegates to the current store, so
// a corrupt-registry reset can swap the underlying registry without rebuilding
// the listeners and pairing manager that captured this reference. The inert
// registry's literal must name every method, so its keys are the full list.
export function swappableRegistry(get: () => DeviceRegistry): DeviceRegistry {
  const methods = Object.keys(openInertRegistry()) as Array<keyof DeviceRegistry>;
  return Object.fromEntries(
    methods.map((name) => [name, (...args: unknown[]) => (get()[name] as (...a: unknown[]) => unknown)(...args)]),
  ) as unknown as DeviceRegistry;
}
