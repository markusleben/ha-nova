import { describe, expect, it } from "vitest";
import { SignJWT, createLocalJWKSet, exportJWK, generateKeyPair } from "jose";

import {
  CensusStore,
  INSTALLATION_ID_PATTERN,
  InstallationRecord,
  InstallationStats,
  LegacyCounterKey,
  LegacyCounterRow,
  MAX_BODY_BYTES,
  handleMutationRequest,
  hashInstallationID,
  isoWeekUTC,
  readBodyCapped,
  validatePing,
  validateWithdraw,
} from "../../census-worker/src/census.js";
import {
  localStatsAccess,
  verifyCloudflareAccess,
} from "../../census-worker/src/access.js";
import { mutationRateIdentity } from "../../census-worker/src/rate-limit.js";
import { boundedNumberRecord } from "../../census-worker/src/storage-policy.js";
import {
  HA_ANALYTICS_URL,
  RELAY_APP_SLUG,
  buildPrivateStats,
  fetchRelayAppAnalytics,
  renderDashboard,
} from "../../census-worker/src/stats.js";

const NOW = new Date("2026-07-24T12:00:00Z");
const ID = "cns-0123456789abcdef0123456789abcdef";
const SECOND_ID = "cns-fedcba9876543210fedcba9876543210";

function request(
  path: string,
  body: unknown,
  overrides: Partial<{
    method: string;
    contentType: string;
    bodyText: string;
    contentLength: number;
  }> = {},
) {
  return {
    method: "POST",
    path,
    contentType: "application/json",
    bodyText: typeof body === "string" ? body : JSON.stringify(body),
    accessToken: "",
    localStatsToken: "",
    localRequest: false,
    ...overrides,
  };
}

function memoryStore(): CensusStore & {
  installations: Map<string, InstallationRecord>;
  legacy: LegacyCounterRow[];
} {
  const installations = new Map<string, InstallationRecord>();
  const legacy = new Map<string, LegacyCounterRow>();
  return {
    installations,
    legacy: [],
    async upsertInstallation(record): Promise<void> {
      installations.set(record.id_hash, { ...record });
    },
    async deleteInstallation(idHash): Promise<void> {
      installations.delete(idHash);
    },
    async incrementLegacy(key): Promise<void> {
      const id = `${key.iso_week}|${key.version}|${key.os}|${key.relay}`;
      const previous = legacy.get(id);
      legacy.set(id, {
        ...key,
        count: (previous?.count ?? 0) + 1,
      });
      this.legacy = [...legacy.values()];
    },
    async installationStats(): Promise<InstallationStats> {
      throw new Error("not used by mutation tests");
    },
    async legacyRows(): Promise<LegacyCounterRow[]> {
      return [...legacy.values()];
    },
  };
}

describe("Census schema-2 mutation contract", () => {
  it("upserts one hashed record for repeat reports from the same installation", async () => {
    const store = memoryStore();
    const first = {
      schema: 2,
      installation_id: ID,
      version: "0.21.3",
      relay: "0.7.1",
      os: "macos",
    };
    expect(
      (await handleMutationRequest(request("/ping", first), store, NOW)).status,
    ).toBe(204);
    expect(
      (
        await handleMutationRequest(
          request("/ping", { ...first, version: "0.21.4", relay: undefined }),
          store,
          new Date(NOW.getTime() + 8 * 86400000),
        )
      ).status,
    ).toBe(204);
    expect(store.installations).toHaveLength(1);
    const [hash, stored] = [...store.installations.entries()][0] ?? [];
    expect(hash).toBe(await hashInstallationID(ID));
    expect(hash).not.toContain(ID);
    expect(stored).toMatchObject({
      version: "0.21.4",
      os: "macos",
    });
    expect(stored).not.toHaveProperty("installation_id");
    expect(stored).not.toHaveProperty("relay");
  });

  it("counts two different IDs as two installations", async () => {
    const store = memoryStore();
    for (const installation_id of [ID, SECOND_ID]) {
      const result = await handleMutationRequest(
        request("/ping", {
          schema: 2,
          installation_id,
          version: "0.21.3",
          os: "linux",
        }),
        store,
        NOW,
      );
      expect(result.status).toBe(204);
    }
    expect(store.installations).toHaveLength(2);
  });

  it("withdraws only the matching hashed installation", async () => {
    const store = memoryStore();
    for (const installation_id of [ID, SECOND_ID]) {
      await handleMutationRequest(
        request("/ping", {
          schema: 2,
          installation_id,
          version: "0.21.3",
          os: "windows",
        }),
        store,
        NOW,
      );
    }
    const result = await handleMutationRequest(
      request("/withdraw", { schema: 2, installation_id: ID }),
      store,
      NOW,
    );
    expect(result.status).toBe(204);
    expect(store.installations.has(await hashInstallationID(ID))).toBe(false);
    expect(store.installations.has(await hashInstallationID(SECOND_ID))).toBe(
      true,
    );
  });

  it("accepts legacy schema 1 only into separate activity counters", async () => {
    const store = memoryStore();
    const result = await handleMutationRequest(
      request("/ping", {
        schema: 1,
        version: "0.21.2",
        relay: "0.7.0",
        os: "linux",
      }),
      store,
      NOW,
    );
    expect(result.status).toBe(204);
    expect(store.installations).toHaveLength(0);
    expect(store.legacy).toEqual([
      {
        iso_week: isoWeekUTC(NOW),
        version: "0.21.2",
        relay: "0.7.0",
        os: "linux",
        count: 1,
      },
    ]);
  });

  it("strictly rejects bad methods, media types, IDs, schemas, and extra fields", async () => {
    const store = memoryStore();
    expect(
      (
        await handleMutationRequest(
          request("/ping", {}, { method: "GET" }),
          store,
          NOW,
        )
      ).status,
    ).toBe(405);
    expect(
      (
        await handleMutationRequest(
          request("/ping", {}, { contentType: "text/plain" }),
          store,
          NOW,
        )
      ).status,
    ).toBe(415);
    const invalid = [
      {
        schema: 2,
        installation_id: "client-1",
        version: "0.21.3",
        os: "macos",
      },
      { schema: 3, installation_id: ID, version: "0.21.3", os: "macos" },
      { schema: 2, installation_id: ID, version: "latest", os: "macos" },
      { schema: 2, installation_id: ID, version: "0.21.3", os: "freebsd" },
      {
        schema: 2,
        installation_id: ID,
        version: "0.21.3",
        os: "macos",
        ip: "x",
      },
    ];
    for (const body of invalid) {
      expect(
        (await handleMutationRequest(request("/ping", body), store, NOW))
          .status,
        JSON.stringify(body),
      ).toBe(400);
    }
    expect(
      (
        await handleMutationRequest(
          request("/withdraw", { schema: 2, installation_id: ID, reason: "x" }),
          store,
          NOW,
        )
      ).status,
    ).toBe(400);
  });

  it("enforces the byte cap for declared and streamed bodies", async () => {
    const store = memoryStore();
    expect(
      (
        await handleMutationRequest(
          request("/ping", "", { contentLength: MAX_BODY_BYTES + 1 }),
          store,
          NOW,
        )
      ).status,
    ).toBe(413);
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("x".repeat(400)));
        controller.enqueue(new TextEncoder().encode("y".repeat(400)));
        controller.close();
      },
    });
    expect(await readBodyCapped(stream, MAX_BODY_BYTES)).toEqual({
      text: "",
      overflow: true,
    });
  });

  it("keeps validation helpers aligned with the wire grammar", () => {
    expect(
      validatePing(
        JSON.stringify({
          schema: 2,
          installation_id: ID,
          version: "0.21.3-rc2",
          os: "windows",
        }),
      ).ok,
    ).toBe(true);
    expect(
      validateWithdraw(JSON.stringify({ schema: 2, installation_id: ID })).ok,
    ).toBe(true);
    expect(INSTALLATION_ID_PATTERN.test(ID)).toBe(true);
  });
});

describe("private maintainer statistics", () => {
  const client: InstallationStats = {
    active_21_days: 7,
    known_60_days: 9,
    by_version: { "0.21.3": 6, "0.21.2": 1 },
    by_os: { macos: 4, linux: 2, windows: 1 },
    relay_versions: { "0.7.1": 5 },
    relay_not_recently_observed: 2,
    new_installation_rejections_today: 0,
  };

  it("keeps client installs, Relay App installs, and legacy pings separate", () => {
    const stats = buildPrivateStats(
      client,
      [
        {
          iso_week: "2026-W30",
          version: "0.21.2",
          os: "macos",
          relay: "not-reported",
          count: 12,
        },
      ],
      {
        status: "available",
        source: HA_ANALYTICS_URL,
        slug: RELAY_APP_SLUG,
        total: 9,
        by_version: { "0.7.0": 7, "0.6.0": 2 },
      },
      NOW,
    );
    expect(stats.client_installations.active_21_days).toBe(7);
    expect(stats.relay_app_installations.total).toBe(9);
    expect(stats.legacy_ping_activity.weekly).toEqual([
      { iso_week: "2026-W30", count: 12 },
    ]);
    expect(stats.relay_app_installations.counting_note).toContain(
      "must not be added",
    );
    expect(JSON.stringify(stats)).not.toContain("id_hash");
  });

  it("labels absent Relay observations plainly instead of inventing a version", () => {
    const stats = buildPrivateStats(
      client,
      [],
      {
        status: "unavailable",
        source: HA_ANALYTICS_URL,
        slug: RELAY_APP_SLUG,
        error: "offline",
      },
      NOW,
    );
    const html = renderDashboard(stats);
    expect(html).toContain(
      "did not report a Relay version observed within the previous 14 days",
    );
    expect(html).not.toContain("unknown Relay version");
    expect(html).toContain("Official Relay App installations");
    expect(html).toContain("Machine-readable data");
  });

  it("reads the official Home Assistant Analytics add-on slug", async () => {
    const analytics = await fetchRelayAppAnalytics(async (input) => {
      expect(String(input)).toBe(HA_ANALYTICS_URL);
      return Response.json({
        [RELAY_APP_SLUG]: {
          total: 9,
          versions: { "0.7.0": 7, "0.6.0": 1, "0.2.0": 1 },
        },
      });
    });
    expect(analytics).toMatchObject({
      status: "available",
      total: 9,
      by_version: { "0.7.0": 7, "0.6.0": 1, "0.2.0": 1 },
    });
  });

  it("reports Analytics failure explicitly instead of using stale invented data", async () => {
    const analytics = await fetchRelayAppAnalytics(async () => {
      return new Response(null, { status: 503 });
    });
    expect(analytics.status).toBe("unavailable");
    expect(analytics).not.toHaveProperty("total");
  });

  it.each([
    { total: -1, versions: { "0.7.0": -1 } },
    { total: 2, versions: { "0.7.0": 1 } },
    { total: 1, versions: { "0.7.0": -1 } },
  ])("rejects inconsistent Analytics counts: %j", async (entry) => {
    const analytics = await fetchRelayAppAnalytics(async () =>
      Response.json({ [RELAY_APP_SLUG]: entry }),
    );
    expect(analytics.status).toBe("unavailable");
    expect(analytics).not.toHaveProperty("total");
  });

  it("allows the local stats bypass only when an exact token is configured", () => {
    expect(localStatsAccess("test", "test")).toBe(true);
    expect(localStatsAccess("test", "other")).toBe(false);
    expect(localStatsAccess("", "")).toBe(false);
    expect(localStatsAccess("test", undefined)).toBe(false);
  });

  it("validates Access issuer, audience, signature, and expiry", async () => {
    const issuer = "https://ha-nova.cloudflareaccess.com";
    const audience = "census-stats-aud";
    const { privateKey, publicKey } = await generateKeyPair("RS256");
    const publicJWK = await exportJWK(publicKey);
    publicJWK.kid = "access-test";
    const keySet = createLocalJWKSet({ keys: [publicJWK] });
    const sign = (
      overrides: { issuer?: string; audience?: string; expires?: string } = {},
    ) =>
      new SignJWT({ type: "service_token" })
        .setProtectedHeader({ alg: "RS256", kid: "access-test" })
        .setIssuer(overrides.issuer ?? issuer)
        .setAudience(overrides.audience ?? audience)
        .setIssuedAt()
        .setExpirationTime(overrides.expires ?? "5m")
        .sign(privateKey);

    expect(
      await verifyCloudflareAccess(await sign(), issuer, audience, keySet),
    ).toBe(true);
    expect(
      await verifyCloudflareAccess(
        await sign({ audience: "wrong" }),
        issuer,
        audience,
        keySet,
      ),
    ).toBe(false);
    expect(
      await verifyCloudflareAccess(
        await sign({ issuer: "https://wrong.cloudflareaccess.com" }),
        issuer,
        audience,
        keySet,
      ),
    ).toBe(false);
    expect(
      await verifyCloudflareAccess(
        await sign({ expires: "0s" }),
        issuer,
        audience,
        keySet,
      ),
    ).toBe(false);
    const otherKeys = await generateKeyPair("RS256");
    const wrongSignature = await new SignJWT({})
      .setProtectedHeader({ alg: "RS256", kid: "access-test" })
      .setIssuer(issuer)
      .setAudience(audience)
      .setExpirationTime("5m")
      .sign(otherKeys.privateKey);
    expect(
      await verifyCloudflareAccess(wrongSignature, issuer, audience, keySet),
    ).toBe(false);
  });

  it("isolates valid ping and withdrawal quotas by route and hashed ID", async () => {
    const ping = request("/ping", {
      schema: 2,
      installation_id: ID,
      version: "0.21.3",
      os: "linux",
    });
    const withdraw = request("/withdraw", {
      schema: 2,
      installation_id: ID,
    });
    const otherPing = request("/ping", {
      schema: 2,
      installation_id: SECOND_ID,
      version: "0.21.3",
      os: "linux",
    });
    const invalid = request("/withdraw", {
      schema: 2,
      installation_id: "invalid",
    });
    const pingIdentity = await mutationRateIdentity(ping);
    const withdrawIdentity = await mutationRateIdentity(withdraw);
    const otherIdentity = await mutationRateIdentity(otherPing);
    expect(pingIdentity?.route).toBe("ping");
    expect(withdrawIdentity?.route).toBe("withdraw");
    expect(pingIdentity?.key).toBe(withdrawIdentity?.key);
    expect(otherIdentity?.key).not.toBe(pingIdentity?.key);
    expect(await mutationRateIdentity(invalid)).toBeUndefined();
    expect(
      await mutationRateIdentity(request("/ping", {}, { method: "GET" })),
    ).toBeUndefined();
    expect(
      await mutationRateIdentity(
        request("/ping", {
          schema: 1,
          version: "0.21.2",
          os: "linux",
        }),
      ),
    ).toBeUndefined();
  });

  it("bounds high-cardinality breakdowns with an explicit other bucket", () => {
    expect(
      boundedNumberRecord(
        [
          { version: "0.21.3", count: 7 },
          { version: "0.21.2", count: 2 },
        ],
        "version",
        12,
      ),
    ).toEqual({ "0.21.3": 7, "0.21.2": 2, other: 3 });
  });
});
