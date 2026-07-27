import {
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  RelayInstanceIdentityError,
  isRelayInstanceId,
  loadOrCreateRelayInstanceId,
} from "../../nova/src/storage/relay-instance.js";

describe("relay instance identity", () => {
  let dir: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "ha-nova-relay-id-"));
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("creates one persistent random owner-only identity", () => {
    const first = loadOrCreateRelayInstanceId(dir);
    const second = loadOrCreateRelayInstanceId(dir);

    expect(second).toBe(first);
    expect(isRelayInstanceId(first)).toBe(true);
    expect(readFileSync(join(dir, "relay-instance-id"), "utf8")).toBe(first);
    expect(statSync(join(dir, "relay-instance-id")).mode & 0o777).toBe(0o600);
  });

  it("uses different identities for different Relay data directories", () => {
    const other = mkdtempSync(join(tmpdir(), "ha-nova-relay-id-other-"));
    try {
      expect(loadOrCreateRelayInstanceId(other)).not.toBe(
        loadOrCreateRelayInstanceId(dir),
      );
    } finally {
      rmSync(other, { recursive: true, force: true });
    }
  });

  it("fails closed instead of rotating a present corrupt identity", () => {
    const path = join(dir, "relay-instance-id");
    writeFileSync(path, "not-a-relay-id", { mode: 0o600 });
    expect(() => loadOrCreateRelayInstanceId(dir)).toThrow(
      RelayInstanceIdentityError,
    );
    expect(readFileSync(path, "utf8")).toBe("not-a-relay-id");
  });
});
