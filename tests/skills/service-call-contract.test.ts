// tests/skills/service-call-contract.test.ts
import { describe, it, expect } from "vitest";

describe("service call contract", () => {
  describe("service call request shape via /core", () => {
    it("calls a service with entity target", () => {
      const request = {
        method: "POST" as const,
        path: "/api/services/light/turn_on",
        body: {
          entity_id: "light.living_room",
        },
      };
      expect(request.method).toBe("POST");
      expect(request.path).toMatch(/^\/api\/services\/\w+\/\w+$/);
      expect(request.body.entity_id).toBe("light.living_room");
    });

    it("supports service data fields", () => {
      const request = {
        method: "POST" as const,
        path: "/api/services/light/turn_on",
        body: {
          entity_id: "light.kitchen",
          brightness: 128,
          color_temp: 350,
        },
      };
      expect(request.body).toHaveProperty("brightness");
      expect(request.body).toHaveProperty("color_temp");
    });

    it("supports multiple entity targets", () => {
      const request = {
        method: "POST" as const,
        path: "/api/services/light/turn_off",
        body: {
          entity_id: ["light.living_room", "light.kitchen"],
        },
      };
      expect(Array.isArray(request.body.entity_id)).toBe(true);
    });

    it("supports area_id target", () => {
      const request = {
        method: "POST" as const,
        path: "/api/services/light/turn_on",
        body: {
          area_id: "living_room",
        },
      };
      expect(request.body).toHaveProperty("area_id");
    });

    it("supports return_response query parameter", () => {
      const request = {
        method: "POST" as const,
        path: "/api/services/weather/get_forecasts?return_response",
        body: {
          entity_id: "weather.home",
          type: "daily",
        },
      };
      expect(request.path).toContain("return_response");
    });
  });

  describe("service list request shape via /core", () => {
    it("lists all available services", () => {
      const request = {
        method: "GET" as const,
        path: "/api/services",
      };
      expect(request.method).toBe("GET");
      expect(request.path).toBe("/api/services");
    });
  });
});
