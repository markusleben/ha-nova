import type {
  InstallationRecord,
  InstallationStats,
  LegacyCounterKey,
  LegacyCounterRow,
} from "./census.js";

export type InstallationUpsertResult =
  | { ok: true }
  | {
      ok: false;
      status: 429 | 507;
      error:
        | "new installation admission limit reached"
        | "installation capacity reached";
    };

export interface CensusStore {
  upsertInstallation(
    record: InstallationRecord,
  ): Promise<InstallationUpsertResult>;
  deleteInstallation(idHash: string): Promise<void>;
  incrementLegacy(key: LegacyCounterKey): Promise<void>;
  installationStats(now: Date): Promise<InstallationStats>;
  legacyRows(now: Date): Promise<LegacyCounterRow[]>;
}

export function censusStoreFor<
  Id,
  Namespace extends {
    idFromName(name: string): Id;
    get(id: Id): {
      fetch(request: RequestInfo | URL, init?: RequestInit): Promise<Response>;
    };
  },
>(env: { CENSUS: Namespace }): CensusStore {
  const stub = env.CENSUS.get(env.CENSUS.idFromName("public-v0.21"));
  const checked = async (
    request: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const response = await stub.fetch(request, init);
    if (!response.ok) {
      throw new Error(`Census storage HTTP ${response.status}`);
    }
    return response;
  };
  return {
    async upsertInstallation(record) {
      const response = await stub.fetch("https://census-do/installation", {
        method: "POST",
        body: JSON.stringify(record),
      });
      if (response.status === 429) {
        return {
          ok: false,
          status: 429,
          error: "new installation admission limit reached",
        };
      }
      if (response.status === 507) {
        return {
          ok: false,
          status: 507,
          error: "installation capacity reached",
        };
      }
      if (!response.ok) {
        throw new Error(`Census storage HTTP ${response.status}`);
      }
      return { ok: true };
    },
    async deleteInstallation(idHash): Promise<void> {
      await checked("https://census-do/installation", {
        method: "DELETE",
        body: JSON.stringify({ id_hash: idHash }),
      });
    },
    async incrementLegacy(key): Promise<void> {
      await checked("https://census-do/legacy", {
        method: "POST",
        body: JSON.stringify(key),
      });
    },
    async installationStats(now): Promise<InstallationStats> {
      const response = await checked(
        `https://census-do/stats?now=${now.getTime()}`,
      );
      return (await response.json()) as InstallationStats;
    },
    async legacyRows(now): Promise<LegacyCounterRow[]> {
      const response = await checked(
        `https://census-do/legacy?now=${now.getTime()}`,
      );
      return (await response.json()) as LegacyCounterRow[];
    },
  };
}
