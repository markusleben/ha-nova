import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// The hosted demo builds the chat bridge (scripts/demo/hosted/Dockerfile.bridge)
// in the golang image pinned in host-lib.sh. That image builds with
// GOTOOLCHAIN=local, so it must carry exactly the Go that cli/go.mod names.
describe("hosted demo pins", () => {
  it("builds the chat bridge with the Go version cli/go.mod names", () => {
    const go = readFileSync("cli/go.mod", "utf8").match(/^go (\S+)$/m)?.[1];
    const pin = readFileSync("scripts/demo/hosted/host-lib.sh", "utf8").match(/^GO_IMAGE=golang:(\S+)@sha256:[0-9a-f]{64}$/m)?.[1];
    expect(go).toBeTruthy();
    expect(pin).toBe(go);
  });
});
