import { describe, expect, it } from "vitest";

import { BRIDGE_INSTALL_ID, decodePairingMetadata } from "../../nova/src/security/pairing-metadata.js";

// The chat-app bridge's install id is the App's own: no pairing may claim it.
describe("pairing metadata", () => {
  it("refuses the chat-app bridge's reserved install id", () => {
    const meta = (id: string) => Buffer.from(JSON.stringify({ name: "n", platform: "p", client: "c", client_install_id: id }));
    expect(decodePairingMetadata(meta("laptop-1"))).not.toBeNull();
    expect(decodePairingMetadata(meta(BRIDGE_INSTALL_ID))).toBeNull();
  });
});
