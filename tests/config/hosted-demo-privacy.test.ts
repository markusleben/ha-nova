import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// What the privacy policy (hanova.app/datenschutz.html) says about the hosted
// demo, pinned in its Caddyfile (docs/reference/hosted-demo.md, Logs / privacy).
const caddy = readFileSync("scripts/demo/hosted/Caddyfile", "utf8");

describe("hosted demo privacy", () => {
  it("rolls the access log daily by the clock and keeps rolled files 4 days", () => {
    const writer = caddy.match(/output file \/logs\/access\.log \{([^}]*)\}/)?.[1] ?? "";
    expect(writer).toMatch(/^\s*roll_at 00:00$/m);
    expect(writer).toMatch(/^\s*roll_keep_for 96h$/m);
  });

  it("hands Home Assistant only the truncated client IPv4", () => {
    expect(caddy).toMatch(/reverse_proxy homeassistant:8123 \{[^}]*header_up X-Forwarded-For \{masked_client_ip\}/);
    const [, pattern, replacement] = caddy.match(/map \{client_ip\} \{masked_client_ip\} \{\s*~(\S+) "([^"]+)"\s*default 192\.0\.2\.0\s*\}/) ?? [];
    const mask = (ip: string) => (new RegExp(pattern).test(ip) ? ip.replace(new RegExp(pattern), replacement.replace("${1}", "$1")) : "192.0.2.0");
    expect(mask("203.0.113.77")).toBe("203.0.113.0");
    expect(mask("10.89.240.130")).toBe("10.89.240.0");
    expect(mask("2001:db8::1")).toBe("192.0.2.0");
  });

  it("drops visitor requests from Caddy's own log and sends no referrer", () => {
    expect(caddy).toMatch(/log default \{\s*format filter \{\s*wrap json\s*request delete\s*\}/);
    expect(caddy).toContain('Referrer-Policy "no-referrer"');
  });

  it("links the legal pages from the restarting page, /abuse, and the banner", () => {
    const banner = readFileSync("scripts/demo/hosted/demo.mjs", "utf8");
    for (const page of ["impressum", "datenschutz"]) {
      const url = `https://hanova.app/${page}.html`;
      expect(caddy.split(url).length - 1).toBe(2);
      expect(banner).toContain(`(${url})`);
    }
  });
});
