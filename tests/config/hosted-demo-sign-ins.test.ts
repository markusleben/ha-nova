import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

// The chat-app sign-ins the hosted demo carries across its hourly reset
// (scripts/demo/hosted/chat.mjs keep/restore, reset.sh).
const CHAT = "scripts/demo/hosted/chat.mjs";
const OLD_DEMO = "a".repeat(32);
const NEW_DEMO = "b".repeat(32);
const CLAUDE = "https://claude.ai/oauth/mcp-oauth-client-metadata";
const CHATGPT = "https://chatgpt.com/oauth/abc123/client.json";
const hex = (c: string, n: number) => c.repeat(n);
const ago = (hours: number) => new Date(Date.now() - hours * 3600_000).toISOString();

let dirs: string[] = [];
afterEach(() => {
  for (const d of dirs) rmSync(d, { recursive: true, force: true });
  dirs = [];
});

function stateDir(demo: string, tokens: object[], credentials: object[] = []) {
  const dir = mkdtempSync(join(tmpdir(), "ha-nova-sign-ins-"));
  dirs.push(dir);
  mkdirSync(join(dir, "config/.storage"), { recursive: true });
  writeFileSync(join(dir, "demo-users.json"), JSON.stringify({ demo }));
  writeFileSync(join(dir, "config/.storage/auth"), JSON.stringify({
    version: 1, minor_version: 1, key: "auth", data: { users: [], groups: [], credentials, refresh_tokens: tokens },
  }));
  return dir;
}

const token = (id: string, over: object = {}) => ({
  id: hex(id, 32), user_id: OLD_DEMO, client_id: CLAUDE, client_name: null, client_icon: null, token_type: "normal",
  created_at: ago(30), access_token_expiration: 1800, token: hex(id, 128), jwt_key: hex(id, 128),
  last_used_at: ago(1), last_used_ip: "203.0.113.0", expire_at: 1, credential_id: "old", version: "2026.10.1", ...over,
});

const run = (...args: string[]) => execFileSync("node", [CHAT, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });

describe("hosted demo: chat-app sign-ins across the reset", () => {
  it("keeps only the demo user's recent chat-app tokens and restores them bare, bound to the new demo user", () => {
    const old = stateDir(OLD_DEMO, [
      token("1"), // Claude, used an hour ago
      token("2", { client_id: CHATGPT, client_name: "Visit evil.example", last_used_ip: "198.51.100.0", access_token_expiration: 9e9 }),
      token("3", { last_used_at: ago(25) }), // stale
      token("4", { client_id: "https://demo.hanova.app/" }), // browser session
      token("5", { token_type: "long_lived_access_token", client_id: null }),
      token("6", { user_id: "c".repeat(32) }), // another user
      token("7", { client_id: "https://claude.ai.evil.example/x" }),
      token("8", { token: "short" }),
    ]);
    const carry = join(old, "carry.json");
    run("keep", old, carry);
    expect(JSON.parse(readFileSync(carry, "utf8")).map((t: { id: string }) => t.id[0])).toEqual(["1", "2"]);

    const fresh = stateDir(NEW_DEMO, [token("9", { user_id: "d".repeat(32), client_id: "http://127.0.0.1:18123/" })],
      [{ id: "newcred", user_id: NEW_DEMO, auth_provider_type: "homeassistant", auth_provider_id: null, data: { username: "demo" } }]);
    expect(run("restore", carry, fresh).trim()).toBe("2");
    const tokens = JSON.parse(readFileSync(join(fresh, "config/.storage/auth"), "utf8")).data.refresh_tokens;
    const chatgpt = tokens.find((t: { client_id: string }) => t.client_id === CHATGPT);
    expect(chatgpt).toMatchObject({
      user_id: NEW_DEMO, client_name: null, client_icon: null, token_type: "normal", access_token_expiration: 1800,
      last_used_ip: null, credential_id: "newcred", version: "2026.10.1", token: hex("2", 128), jwt_key: hex("2", 128),
    });
    expect(tokens).toHaveLength(3);
    // Twice is harmless: no duplicates.
    expect(run("restore", carry, fresh).trim()).toBe("0");
  });

  it("caps the kept tokens at the newest 200", () => {
    const many = Array.from({ length: 250 }, (_, i) => token("e", {
      id: i.toString(16).padStart(32, "0"), last_used_at: ago(i / 100),
    }));
    const old = stateDir(OLD_DEMO, many);
    const carry = join(old, "carry.json");
    run("keep", old, carry);
    const kept = JSON.parse(readFileSync(carry, "utf8"));
    expect(kept).toHaveLength(200);
    expect(kept[0].id).toBe("0".repeat(32));
  });

  it("follows no link and keeps the last carry file when the old store is unreadable", () => {
    const old = stateDir(OLD_DEMO, [token("1")]);
    const carry = join(old, "carry.json");
    run("keep", old, carry);
    const before = readFileSync(carry, "utf8");
    rmSync(join(old, "config/.storage/auth"));
    symlinkSync(join(old, "demo-users.json"), join(old, "config/.storage/auth"));
    run("keep", old, carry);
    expect(readFileSync(carry, "utf8")).toBe(before);
  });
});
