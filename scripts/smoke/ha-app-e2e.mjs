#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const HELP = `Usage:
  node scripts/smoke/ha-app-e2e.mjs [--apply]

Environment:
  SUPERVISOR_TOKEN   Required. Token for Supervisor API.
  SUPERVISOR_URL     Optional. Default: http://supervisor
  APP_SLUG           Optional. Default: self
  BRIDGE_BASE_URL    Required. Base URL for ha-nova bridge (example: http://homeassistant.local:8791)
  BRIDGE_AUTH_TOKEN  Required. Bridge auth bearer token.
  HA_LLAT            Optional. LLAT to seed in app options (used when --apply).
  WS_TYPE            Optional. Default: ping
  Also loaded (if present): .env.local, .env

Behavior:
  - Reads current app options.
  - Validates merged options via Supervisor API.
  - With --apply: writes options + restarts app.
  - Verifies /health and /ws against current runtime.
`;

const args = process.argv.slice(2);
const apply = args.includes("--apply");
loadLocalEnv();

if (args.includes("-h") || args.includes("--help")) {
  console.log(HELP);
  process.exit(0);
}

const supervisorToken = readRequired("SUPERVISOR_TOKEN");
const bridgeBaseUrl = stripTrailingSlash(readRequired("BRIDGE_BASE_URL"));
const bridgeAuthToken = readRequired("BRIDGE_AUTH_TOKEN");

const supervisorUrl = stripTrailingSlash(readOptional("SUPERVISOR_URL") ?? "http://supervisor");
const appSlug = readOptional("APP_SLUG") ?? "self";
const requestedLlat = readOptional("HA_LLAT");
const wsType = readOptional("WS_TYPE") ?? "ping";

const supervisorHeaders = {
  authorization: `Bearer ${supervisorToken}`,
  "content-type": "application/json"
};

const appInfo = await requestJson(`${supervisorUrl}/addons/${appSlug}/info`, {
  method: "GET",
  headers: supervisorHeaders
});

const currentOptions = toObject(appInfo.data?.options);
const nextOptions = {
  ...currentOptions,
  bridge_auth_token: bridgeAuthToken
};

if (requestedLlat) {
  nextOptions.ha_llat = requestedLlat;
}

await requestJson(`${supervisorUrl}/addons/${appSlug}/options/validate`, {
  method: "POST",
  headers: supervisorHeaders,
  body: JSON.stringify(nextOptions)
});

if (apply) {
  await requestJson(`${supervisorUrl}/addons/${appSlug}/options`, {
    method: "POST",
    headers: supervisorHeaders,
    body: JSON.stringify({ options: nextOptions })
  });

  await requestJson(`${supervisorUrl}/addons/${appSlug}/restart`, {
    method: "POST",
    headers: supervisorHeaders
  });
}

const runtimeHasLlat = apply
  ? Boolean(readOptionalFromUnknown(nextOptions.ha_llat))
  : Boolean(readOptionalFromUnknown(currentOptions.ha_llat));

const health = await waitForHealth({
  baseUrl: bridgeBaseUrl,
  authToken: bridgeAuthToken,
  maxAttempts: apply ? 30 : 5,
  delayMs: 1000
});

const ws = await callWs({
  baseUrl: bridgeBaseUrl,
  authToken: bridgeAuthToken,
  wsType
});

assertWsExpectation({
  ws,
  expectFullScope: runtimeHasLlat
});

console.log(
  JSON.stringify(
    {
      ok: true,
      app_slug: appSlug,
      apply,
      runtime_has_llat: runtimeHasLlat,
      health_status: health.status,
      ws_status: ws.status
    },
    null,
    2
  )
);

async function waitForHealth(input) {
  const { baseUrl, authToken, maxAttempts, delayMs } = input;

  let lastError = null;
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    try {
      const response = await fetch(`${baseUrl}/health`, {
        method: "GET",
        headers: {
          authorization: `Bearer ${authToken}`
        },
        signal: AbortSignal.timeout(8_000)
      });

      if (response.ok) {
        const body = await parseJson(response);
        return { status: response.status, body };
      }

      lastError = new Error(`health status ${response.status}`);
    } catch (error) {
      lastError = error;
    }

    if (attempt < maxAttempts) {
      await sleep(delayMs);
    }
  }

  const message = lastError instanceof Error ? lastError.message : "unknown health error";
  fail(`Health check failed after ${maxAttempts} attempts: ${message}`);
}

async function callWs(input) {
  const { baseUrl, authToken, wsType } = input;

  const response = await fetch(`${baseUrl}/ws`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${authToken}`,
      "content-type": "application/json"
    },
    body: JSON.stringify({ type: wsType }),
    signal: AbortSignal.timeout(8_000)
  });

  const body = await parseJson(response);
  return {
    status: response.status,
    body
  };
}

function assertWsExpectation(input) {
  const { ws, expectFullScope } = input;

  if (expectFullScope) {
    if (ws.status !== 200) {
      fail(`Expected /ws status 200 with LLAT, got ${ws.status}: ${JSON.stringify(ws.body)}`);
    }
    return;
  }

  const degraded =
    ws.status === 502
    && ws.body
    && typeof ws.body === "object"
    && ws.body.error
    && ws.body.error.code === "UPSTREAM_WS_ERROR";

  if (ws.status !== 200 && !degraded) {
    fail(`Expected /ws status 200 or degraded 502, got ${ws.status}: ${JSON.stringify(ws.body)}`);
  }
}

async function requestJson(url, init) {
  const response = await fetch(url, {
    ...init,
    signal: AbortSignal.timeout(12_000)
  });

  const body = await parseJson(response);
  if (!response.ok) {
    fail(`Supervisor call failed (${response.status}) ${url}: ${JSON.stringify(body)}`);
  }

  return toObject(body);
}

async function parseJson(response) {
  const text = await response.text();
  if (!text) {
    return null;
  }

  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

function readRequired(name) {
  const value = readOptional(name);
  if (!value) {
    fail(`${name} is required`);
  }
  return value;
}

function readOptional(name) {
  const value = process.env[name]?.trim();
  if (!value) {
    return undefined;
  }
  return value;
}

function readOptionalFromUnknown(value) {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  return trimmed;
}

function toObject(value) {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value;
  }
  return {};
}

function stripTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(message) {
  console.error(`[ha-app-e2e] ${message}`);
  process.exit(1);
}

function loadLocalEnv() {
  const root = process.cwd();
  const files = [resolve(root, ".env.local"), resolve(root, ".env")];
  for (const file of files) {
    if (!existsSync(file)) {
      continue;
    }

    parseEnvFile(file);
  }
}

function parseEnvFile(path) {
  const content = readFileSync(path, "utf8");
  const allowed = new Set([
    "SUPERVISOR_TOKEN",
    "SUPERVISOR_URL",
    "APP_SLUG",
    "BRIDGE_BASE_URL",
    "BRIDGE_AUTH_TOKEN",
    "HA_LLAT",
    "WS_TYPE"
  ]);

  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const idx = line.indexOf("=");
    if (idx <= 0) {
      continue;
    }

    const key = line.slice(0, idx).trim();
    if (!allowed.has(key)) {
      continue;
    }

    const existing = process.env[key];
    if (existing && existing.trim().length > 0) {
      continue;
    }

    let value = line.slice(idx + 1).trim();
    if (
      (value.startsWith("\"") && value.endsWith("\""))
      || (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
