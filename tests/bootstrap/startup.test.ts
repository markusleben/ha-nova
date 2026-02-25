import { describe, expect, it } from "vitest";

import { createApp } from "../../src/index.js";

describe("startup bootstrap", () => {
  it("exports createApp factory", () => {
    expect(typeof createApp).toBe("function");
  });
});
