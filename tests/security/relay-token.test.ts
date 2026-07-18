import {
  chmodSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  symlinkSync,
  writeFileSync
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  loadOrCreateRelayAuthToken,
  resolveRelayAuthToken
} from "../../nova/src/config/relay-token.js";

describe("relay auth token persistence", () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    for (const directory of tempDirs) {
      rmSync(directory, { recursive: true, force: true });
    }
    tempDirs.length = 0;
  });

  function tokenPath(): string {
    const directory = mkdtempSync(join(tmpdir(), "ha-nova-relay-token-"));
    tempDirs.push(directory);
    return join(directory, "relay_auth_token");
  }

  it("creates and reuses a private 32-byte token", () => {
    const path = tokenPath();

    const created = loadOrCreateRelayAuthToken(path);
    const reused = loadOrCreateRelayAuthToken(path);

    expect(created).toMatch(/^[a-f0-9]{64}$/);
    expect(reused).toBe(created);
    expect(readFileSync(path, "utf8")).toBe(`${created}\n`);
    if (process.platform !== "win32") {
      expect(statSync(path).mode & 0o777).toBe(0o600);
    }
  });

  it("keeps a configured legacy token and does not create a file", () => {
    const path = tokenPath();

    const token = resolveRelayAuthToken({
      RELAY_AUTH_TOKEN: "  legacy-token  ",
      RELAY_AUTH_TOKEN_FILE: path
    });

    expect(token).toBe("legacy-token");
    expect(() => statSync(path)).toThrow();
  });

  it("does not auto-create a shared token in App mode (SUPERVISOR_TOKEN, no configured token)", () => {
    // App mode authenticates per device via pairing; auto-creating a shared
    // token would be imported as a spurious legacy credential and churn a
    // plaintext file on every restart after revoke.
    const path = tokenPath();

    const token = resolveRelayAuthToken({
      SUPERVISOR_TOKEN: "supervisor-abc",
      RELAY_AUTH_TOKEN_FILE: path
    });

    expect(token).toBe("");
    expect(() => statSync(path)).toThrow(); // no file created
  });

  it("still honors an explicit token in App mode (a genuine pre-pairing credential)", () => {
    const path = tokenPath();

    const token = resolveRelayAuthToken({
      SUPERVISOR_TOKEN: "supervisor-abc",
      RELAY_AUTH_TOKEN: "real-legacy-token",
      RELAY_AUTH_TOKEN_FILE: path
    });

    expect(token).toBe("real-legacy-token");
    expect(() => statSync(path)).toThrow();
  });

  it("loads an existing token and restores owner-only permissions", () => {
    const path = tokenPath();
    writeFileSync(path, "persisted-token\n", { mode: 0o644 });
    chmodSync(path, 0o644);

    expect(resolveRelayAuthToken({ RELAY_AUTH_TOKEN_FILE: path })).toBe("persisted-token");
    if (process.platform !== "win32") {
      expect(statSync(path).mode & 0o777).toBe(0o600);
    }
  });

  it("treats a literal null App option as absent and creates the persisted token", () => {
    const path = tokenPath();

    const token = resolveRelayAuthToken({
      RELAY_AUTH_TOKEN: "null",
      RELAY_AUTH_TOKEN_FILE: path
    });

    expect(token).toMatch(/^[a-f0-9]{64}$/);
    expect(readFileSync(path, "utf8").trim()).toBe(token);
  });

  it.runIf(process.platform !== "win32")("rejects a symlink instead of reading through it", () => {
    const path = tokenPath();
    const target = `${path}.target`;
    writeFileSync(target, "linked-token\n");
    symlinkSync(target, path);

    expect(() => resolveRelayAuthToken({ RELAY_AUTH_TOKEN_FILE: path })).toThrowError(
      "Relay auth token path must be a small regular file"
    );
  });

  it("fails loud for missing standalone configuration or an empty persisted file", () => {
    expect(() => resolveRelayAuthToken({})).toThrowError("RELAY_AUTH_TOKEN is required");

    const path = tokenPath();
    writeFileSync(path, "\n");
    expect(() => resolveRelayAuthToken({ RELAY_AUTH_TOKEN_FILE: path })).toThrowError(
      "Relay auth token file is empty"
    );
  });
});
