import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// The hosted demo's restarting page serves its own copy of the star (Caddy
// mounts only scripts/demo/hosted/site/), so the copy must not drift.
describe("hosted demo restarting page star", () => {
  it("is the product's assets/icon.svg", () => {
    expect(readFileSync("scripts/demo/hosted/site/icon.svg", "utf8")).toBe(readFileSync("assets/icon.svg", "utf8"));
  });
});
