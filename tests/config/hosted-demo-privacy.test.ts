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

  it("hands Home Assistant and the relay only the truncated client IPv4", () => {
    const site = caddy.slice(caddy.indexOf("{$DEMO_DOMAIN} {"), caddy.indexOf("http://:18123"));
    const proxies = site.match(/reverse_proxy \S+ \{[^}]*/g) ?? [];
    expect(site.split("reverse_proxy ").length - 1).toBe(2);
    expect(proxies).toHaveLength(2);
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

  it("links the privacy page from the restarting page, /abuse, and the banner", () => {
    const banner = readFileSync("scripts/demo/hosted/demo.mjs", "utf8");
    const url = "https://hanova.app/privacy.html";
    expect(caddy.split(url).length - 1).toBe(2);
    expect(banner).toContain(`(${url})`);
  });
});
