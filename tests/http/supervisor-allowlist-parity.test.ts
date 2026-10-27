import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { HIDDEN_VALUE, SUPERVISOR_ROUTES, secretKey } from "../../nova/src/http/handlers/supervisor-allowlist.js";

// The Relay's Supervisor table is the boundary; the CLI mirrors it so a
// refusal names the reason before anything is sent. They must stay equal.
const go = readFileSync(new URL("../../cli/relay_raw_supervisor.go", import.meta.url), "utf8");
const redact = readFileSync(new URL("../../cli/relay_raw_redact.go", import.meta.url), "utf8");

describe("Supervisor allowlist parity (Relay ↔ CLI)", () => {
  it("lists the same routes in the same order", () => {
    const table = go.slice(go.indexOf("var rawSupervisorRoutes"), go.indexOf("var rawSupervisorPatterns"));
    const routes = [...table.matchAll(/\{"(GET|POST)", "([^"]+)", "(\w+)"\}/g)].map(([, method, path, kind]) => ({ method, path, kind }));
    expect(routes).toEqual(SUPERVISOR_ROUTES.map(({ method, path, kind }) => ({ method, path, kind })));
  });

  it("uses the same placeholders, secret words, and hidden marker", () => {
    const ts = readFileSync(new URL("../../nova/src/http/handlers/supervisor-allowlist.ts", import.meta.url), "utf8");
    for (const pattern of ["[A-Za-z0-9][A-Za-z0-9_-]{0,63}", "[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}"]) {
      expect(go).toContain(pattern);
      expect(ts).toContain(pattern);
    }
    const words = (source: string, start: string) => [...source.slice(source.indexOf(start), source.indexOf("}", source.indexOf(start))).matchAll(/"([a-z]+)"/g)].map((m) => m[1]).sort();
    expect(words(go, "var rawSupervisorSecretWords")).toEqual(words(ts, "const SECRET_WORDS").filter((w) => w !== "SECRET_WORDS"));
    expect(go).toContain("password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey");
    expect(ts).toContain("password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey");
    expect(redact).toContain('secretHidden      = "' + HIDDEN_VALUE + '"');
    expect([secretKey("mqtt_password"), secretKey("ha_llat"), secretKey("psk_set"), secretKey("server"), secretKey("log_level")]).toEqual([true, true, true, false, false]);
  });
});
