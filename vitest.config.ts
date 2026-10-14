import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    // nova/ and the repo root each install @serenity-kit/opaque. Two copies
    // mean two WASM instances: tests that await nova's opaqueReady() and then
    // call the root copy race its uninitialized module under parallel load.
    dedupe: ["@serenity-kit/opaque"]
  },
  test: {
    environment: "node",
    include: ["tests/**/*.test.ts"],
    testTimeout: 30000
  }
});
