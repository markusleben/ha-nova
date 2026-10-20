import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// What the privacy policy (hanova.app/privacy.html) says about the hosted
// demo, pinned in its Caddyfile and reset.sh (docs/reference/hosted-demo.md,
// Logs / privacy).
const caddy = readFileSync("scripts/demo/hosted/Caddyfile", "utf8");

describe("hosted demo privacy", () => {
  it("rolls the access log daily by the clock and keeps rolled files 4 days", () => {
    const writer = caddy.match(/output file \/logs\/access\.log \{([^}]*)\}/)?.[1] ?? "";
    expect(writer).toMatch(/^\s*roll_at 00:00$/m);
    expect(writer).toMatch(/^\s*roll_keep_for 96h$/m);
  });

  it("backstops a missed roll at 5 days, a day under the published 6", () => {
    const reset = readFileSync("scripts/demo/hosted/reset.sh", "utf8");
    expect(reset).toContain("if ((ts < $(date +%s) - 5 * 86400)); then");
  });

  it("hands Home Assistant (also on the chat-app path) and the relay only the truncated client IPv4", () => {
    const site = caddy.slice(caddy.indexOf("{$DEMO_DOMAIN} {"), caddy.indexOf("http://:18123"));
    const proxies = site.match(/reverse_proxy \S+ \{[^}]*/g) ?? [];
    expect(site.split("reverse_proxy ").length - 1).toBe(3);
    expect(proxies).toHaveLength(3);
    // [^}]* stops at the placeholder's closing brace.
    for (const proxy of proxies) expect(proxy).toContain("header_up X-Forwarded-For {masked_client_ip");
    const map = caddy.match(/map \{client_ip\} \{masked_client_ip\} \{\s*~(\S+) "([^"]+)"\s*default 192\.0\.2\.0\s*\}/);
    expect(map).not.toBeNull();
    const pattern = new RegExp(map![1]!);
    const replacement = map![2]!.replace("${1}", "$1");
    const mask = (ip: string) => (pattern.test(ip) ? ip.replace(pattern, replacement) : "192.0.2.0");
    expect(mask("203.0.113.77")).toBe("203.0.113.0");
    expect(mask("10.89.240.130")).toBe("10.89.240.0");
    expect(mask("2001:db8::1")).toBe("192.0.2.0");
  });

  it("drops visitor requests from Caddy's own log and sends no referrer", () => {
    expect(caddy).toMatch(/log default \{\s*format filter \{\s*wrap json\s*request delete\s*error regexp \S+ \[ip\]\s*\}/);
    expect(caddy).toContain('Referrer-Policy "no-referrer"');
  });

  it("lets browsers reach no third party but the brand icon CDN, for images and the service worker", () => {
    const csp = caddy.match(/Content-Security-Policy "([^"]+)"/)?.[1] ?? "";
    expect(caddy.split("Content-Security-Policy").length - 1).toBe(1);
    const directives = Object.fromEntries(csp.split("; ").map((d) => [d.split(" ")[0], d.split(" ").slice(1).join(" ")]));
    expect(directives["img-src"]).toBe("'self' data: blob: https://brands.home-assistant.io");
    expect(directives["connect-src"]).toBe("'self' data: blob: https://brands.home-assistant.io");
    // The privacy page names only that CDN: no other origin anywhere in the policy.
    expect(csp.match(/https?:\/\/[^\s;]+/g)?.every((o) => o === "https://brands.home-assistant.io")).toBe(true);
  });

  it("lets Home Assistant's broker clients write only the simulated devices' topics", () => {
    const acl = readFileSync("scripts/demo/hosted/mosquitto.acl", "utf8");
    const writes = acl.split("\n").filter((l) => l.startsWith("topic write ")).map((l) => l.slice(12));
    expect(writes).toEqual(["testbed/+/set", "testbed/+/state", "testbed/plug/factory_reset", "testbed/router/restart"]);
  });

  it("links the privacy page from the restarting page, /abuse, and the banner", () => {
    const banner = readFileSync("scripts/demo/hosted/demo.mjs", "utf8");
    const url = "https://hanova.app/privacy.html";
    expect(caddy.split(url).length - 1).toBe(2);
    expect(banner).toContain(`(${url})`);
  });
});
