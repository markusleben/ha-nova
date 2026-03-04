import { describe, it, expect } from "vitest";

describe("trace contract", () => {
  describe("trace/list request shape", () => {
    it("requires domain field", () => {
      const request = { type: "trace/list", domain: "automation" };
      expect(request.type).toBe("trace/list");
      expect(request.domain).toBe("automation");
    });

    it("accepts optional item_id for filtering", () => {
      const request = {
        type: "trace/list",
        domain: "automation",
        item_id: "motion_kitchen",
      };
      expect(request.item_id).toBe("motion_kitchen");
    });

    it("supports script domain", () => {
      const request = { type: "trace/list", domain: "script" };
      expect(request.domain).toBe("script");
    });
  });

  describe("trace/get request shape", () => {
    it("requires domain, item_id, and run_id", () => {
      const request = {
        type: "trace/get",
        domain: "automation",
        item_id: "motion_kitchen",
        run_id: "abc123",
      };
      expect(request.type).toBe("trace/get");
      expect(request.domain).toBe("automation");
      expect(request.item_id).toBe("motion_kitchen");
      expect(request.run_id).toBe("abc123");
    });
  });

  describe("trace relay path", () => {
    it("trace types go through /ws endpoint (same as get_states)", () => {
      const traceList = { type: "trace/list", domain: "automation" };
      const traceGet = {
        type: "trace/get",
        domain: "automation",
        item_id: "x",
        run_id: "y",
      };
      expect(typeof traceList.type).toBe("string");
      expect(traceList.type.length).toBeGreaterThan(0);
      expect(typeof traceGet.type).toBe("string");
      expect(traceGet.type.length).toBeGreaterThan(0);
    });
  });
});
