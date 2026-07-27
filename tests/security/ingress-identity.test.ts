import type { IncomingMessage } from "node:http";

import { describe, expect, it } from "vitest";

import { resolveIngressIdentity } from "../../nova/src/security/ingress-identity.js";

function request(
  over: {
    peer?: string;
    userId?: string | string[];
    ingressPath?: string | string[];
    rawHeaders?: string[];
  } = {},
): IncomingMessage {
  const userId = over.userId ?? "user-1";
  const ingressPath = over.ingressPath ?? "/api/hassio_ingress/session";
  return {
    socket: { remoteAddress: over.peer ?? "172.30.32.2" },
    headers: {
      "x-remote-user-id": userId,
      "x-ingress-path": ingressPath,
    },
    rawHeaders: over.rawHeaders ?? [
      ...rawHeader("X-Remote-User-Id", userId),
      ...rawHeader("X-Ingress-Path", ingressPath),
    ],
  } as unknown as IncomingMessage;
}

function rawHeader(name: string, value: string | string[]): string[] {
  return (Array.isArray(value) ? value : [value]).flatMap((item) => [
    name,
    item,
  ]);
}

describe("Supervisor ingress identity", () => {
  it("accepts only the exact Supervisor peer with single non-empty headers", () => {
    expect(resolveIngressIdentity(request())).toEqual({
      ok: true,
      userId: "user-1",
      ingressPath: "/api/hassio_ingress/session",
    });
  });

  it("rejects direct requests even when both ingress headers are spoofed", () => {
    expect(resolveIngressIdentity(request({ peer: "127.0.0.1" })).ok).toBe(
      false,
    );
    expect(resolveIngressIdentity(request({ peer: "192.0.2.50" })).ok).toBe(
      false,
    );
  });

  it("rejects missing, joined, array, padded, or control-bearing user identities", () => {
    for (const userId of [
      "",
      "user-1, user-2",
      ["user-1", "user-2"],
      " user-1",
      "user-1\n",
    ]) {
      expect(resolveIngressIdentity(request({ userId })).ok).toBe(false);
    }
  });

  it("rejects duplicate identity and ingress-path headers from raw HTTP", () => {
    expect(
      resolveIngressIdentity(
        request({
          rawHeaders: [
            "X-Remote-User-Id",
            "user-1",
            "X-Remote-User-Id",
            "user-1",
            "X-Ingress-Path",
            "/api/hassio_ingress/session",
          ],
        }),
      ).ok,
    ).toBe(false);
    expect(
      resolveIngressIdentity(
        request({
          rawHeaders: [
            "X-Remote-User-Id",
            "user-1",
            "X-Ingress-Path",
            "/api/hassio_ingress/session",
            "X-Ingress-Path",
            "/api/hassio_ingress/session",
          ],
        }),
      ).ok,
    ).toBe(false);
  });

  it("rejects parsed identity values that have no matching raw header", () => {
    expect(resolveIngressIdentity(request({ rawHeaders: [] })).ok).toBe(false);
  });
});
