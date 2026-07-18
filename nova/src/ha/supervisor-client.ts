// Minimal Supervisor self-API client (App mode only). Reads /addons/self/info
// for the update status shown on the NOVA page and for the effective host port
// the secure device listener is mapped to, and writes /addons/self/options to
// clear a migrated legacy option. Uses the process-local SUPERVISOR_TOKEN; it is
// never reachable in standalone Container/Core, so callers guard on App mode.
//
// Confirmed live against a real Supervisor (hassio_role: default): self/info
// returns 200 with { version, version_latest, update_available, network:
// { "<port>/tcp": <hostPort|null> }, ingress_port }.

const SUPERVISOR_BASE = "http://supervisor";
const REQUEST_TIMEOUT_MS = 10_000;

export interface SelfInfo {
  version: string;
  versionLatest: string | null;
  updateAvailable: boolean;
  // Container port ("8792/tcp") -> mapped host port, or null when unmapped.
  network: Record<string, number | null>;
}

export interface SupervisorClient {
  getSelfInfo(): Promise<SelfInfo>;
  // The effective host port for a container port ("8792/tcp"), or null when the
  // port is not mapped/exposed — the pairing flow treats null as "no secure
  // endpoint" and refuses to activate a code.
  getMappedHostPort(containerPort: string): Promise<number | null>;
  // Replace the App options (used to clear a migrated legacy token). The caller
  // passes the full desired options object.
  setOptions(options: Record<string, unknown>): Promise<void>;
}

export function createSupervisorClient(token: string, base: string = SUPERVISOR_BASE): SupervisorClient {
  async function request(path: string, init?: RequestInit): Promise<unknown> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
    try {
      const response = await fetch(`${base}${path}`, {
        ...init,
        headers: { authorization: `Bearer ${token}`, ...(init?.headers ?? {}) },
        signal: controller.signal,
      });
      const text = await response.text();
      if (!response.ok) {
        throw new Error(`supervisor ${path} returned ${response.status}: ${text.slice(0, 120)}`);
      }
      return text ? JSON.parse(text) : null;
    } finally {
      clearTimeout(timer);
    }
  }

  async function getSelfInfo(): Promise<SelfInfo> {
    const body = await request("/addons/self/info");
    const data = dataOf(body);
    return {
      version: asString(data.version, ""),
      versionLatest: typeof data.version_latest === "string" ? data.version_latest : null,
      updateAvailable: data.update_available === true,
      network: parseNetwork(data.network),
    };
  }

  return {
    getSelfInfo,
    async getMappedHostPort(containerPort) {
      const info = await getSelfInfo();
      const mapped = info.network[containerPort];
      return typeof mapped === "number" ? mapped : null;
    },
    async setOptions(options) {
      await request("/addons/self/options", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ options }),
      });
    },
  };
}

function dataOf(body: unknown): Record<string, unknown> {
  if (typeof body === "object" && body !== null && "data" in body) {
    const data = (body as { data: unknown }).data;
    if (typeof data === "object" && data !== null) {
      return data as Record<string, unknown>;
    }
  }
  throw new Error("unexpected supervisor response shape");
}

function parseNetwork(value: unknown): Record<string, number | null> {
  const out: Record<string, number | null> = {};
  if (typeof value === "object" && value !== null) {
    for (const [key, mapped] of Object.entries(value as Record<string, unknown>)) {
      out[key] = typeof mapped === "number" ? mapped : null;
    }
  }
  return out;
}

function asString(value: unknown, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}
