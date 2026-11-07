import { describe, expect, it } from "vitest";

import { BRIDGE_INSTALL_ID, decodePairingMetadata, GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";

// The chat-app bridge's and the safety-check worker's install ids are the
// App's own: no pairing may claim them. Pairing v1, v2, and Link sign-in all
// decode the device's metadata here.
describe("pairing metadata", () => {
  it("refuses both reserved install ids", () => {
    const meta = (id: string) => Buffer.from(JSON.stringify({ name: "n", platform: "p", client: "c", client_install_id: id }));
    expect(decodePairingMetadata(meta("laptop-1"))).not.toBeNull();
    expect(decodePairingMetadata(meta(BRIDGE_INSTALL_ID))).toBeNull();
    expect(decodePairingMetadata(meta(GATE_INSTALL_ID))).toBeNull();
  });
});
