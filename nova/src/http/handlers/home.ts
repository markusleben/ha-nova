import type { IncomingMessage, ServerResponse } from "node:http";

import type { PairingManager } from "../../security/pairing.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import { readHealthPayload, type HealthHandlerOptions, type HealthPayload } from "./health.js";

const SUPERVISOR_INGRESS_PEERS = new Set([
  "172.30.32.2",
  "::ffff:172.30.32.2",
  "::ffff:ac1e:2002"
]);

export const HOME_CONTENT_SECURITY_POLICY = [
  "default-src 'none'",
  "style-src 'unsafe-inline'",
  "base-uri 'none'",
  "form-action 'none'",
  "frame-ancestors 'self'"
].join("; ");

export interface HomeHandlerOptions {
  health: HealthHandlerOptions;
  pairing: PairingManager;
  requiredRelayVersion: string;
  installerVersion: string;
  now?: () => number;
}

export interface HomePageModel {
  health: HealthPayload;
  pairingCode: string;
  pairingExpiresAtMs: number;
  pairingExpiresInSeconds: number;
  requiredRelayVersion: string;
  installerVersion: string;
}

export function createHomeHandler(options: HomeHandlerOptions): RouteHandler {
  const now = options.now ?? Date.now;

  return async ({ request, response }: RouteContext) => {
    setHomeHeaders(response);
    if (!isSupervisorIngressRequest(request)) {
      throw new HttpError(403, "INGRESS_REQUIRED", "Home Base is available through Supervisor ingress only");
    }

    const health = await readHealthPayload(options.health);
    const pairing = options.pairing.getStatus();
    const page = renderHomePage({
      health,
      pairingCode: pairing.code,
      pairingExpiresAtMs: pairing.expiresAtMs,
      pairingExpiresInSeconds: Math.max(0, Math.ceil((pairing.expiresAtMs - now()) / 1_000)),
      requiredRelayVersion: options.requiredRelayVersion,
      installerVersion: options.installerVersion
    });

    response.statusCode = 200;
    response.setHeader("content-type", "text/html; charset=utf-8");
    response.setHeader("content-length", Buffer.byteLength(page));
    response.end(page);
  };
}

export function isSupervisorIngressRequest(request: IncomingMessage): boolean {
  const peer = request.socket.remoteAddress?.toLowerCase() ?? "";
  const ingressPath = singleHeader(request, "x-ingress-path");
  const userId = singleHeader(request, "x-remote-user-id");
  return SUPERVISOR_INGRESS_PEERS.has(peer) && ingressPath.startsWith("/") && userId.length > 0;
}

export function renderHomePage(model: HomePageModel): string {
  const connection = connectionText(model.health);
  const code = `${model.pairingCode.slice(0, 3)} ${model.pairingCode.slice(3)}`;
  const expiresMinutes = Math.max(1, Math.ceil(model.pairingExpiresInSeconds / 60));
  const commands = installCommands(model.installerVersion);
  const statusClass = model.health.ha_ws_connected ? "good" : "warn";

  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NOVA Home Base</title>
  <style>
    :root { color-scheme: light dark; --bg:#f4f7fb; --panel:#fff; --text:#152033; --muted:#607086; --line:#d9e1eb; --accent:#3768e5; --good:#16815d; --warn:#b56816; }
    @media (prefers-color-scheme: dark) { :root { --bg:#10151d; --panel:#19212c; --text:#edf3fb; --muted:#9daec2; --line:#303c4c; --accent:#7ea2ff; --good:#55d6a3; --warn:#ffbd66; } }
    * { box-sizing:border-box; }
    body { margin:0; background:var(--bg); color:var(--text); font:15px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    main { width:min(920px,calc(100% - 32px)); margin:0 auto; padding:42px 0 56px; }
    header { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; margin-bottom:24px; }
    h1 { margin:0; font-size:clamp(28px,5vw,42px); letter-spacing:-.04em; }
    h2 { margin:0 0 8px; font-size:18px; }
    p { margin:0; color:var(--muted); }
    .eyebrow { color:var(--accent); font-size:12px; font-weight:800; letter-spacing:.14em; text-transform:uppercase; }
    .pill { border:1px solid var(--line); border-radius:999px; padding:7px 11px; white-space:nowrap; font-weight:700; }
    .pill.good { color:var(--good); } .pill.warn { color:var(--warn); }
    .grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
    .card { background:var(--panel); border:1px solid var(--line); border-radius:18px; padding:22px; box-shadow:0 10px 30px rgba(20,35,60,.06); }
    .pairing { grid-column:1/-1; display:grid; grid-template-columns:1.2fr 1fr; gap:28px; align-items:center; }
    .code { margin:12px 0 4px; color:var(--accent); font:800 clamp(42px,10vw,72px)/1 ui-monospace,SFMono-Regular,Consolas,monospace; letter-spacing:.08em; }
    .hint { font-size:13px; }
    dl { display:grid; grid-template-columns:1fr auto; gap:10px 18px; margin:18px 0 0; }
    dt { color:var(--muted); } dd { margin:0; font-weight:700; text-align:right; }
    .install { grid-column:1/-1; }
    pre { margin:12px 0 18px; padding:14px 16px; overflow:auto; border:1px solid var(--line); border-radius:12px; background:var(--bg); color:var(--text); font:12px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace; white-space:pre-wrap; word-break:break-all; }
    footer { margin-top:20px; color:var(--muted); font-size:13px; }
    @media (max-width:680px) { main { padding-top:24px; } header { display:block; } .pill { display:inline-block; margin-top:14px; } .grid,.pairing { grid-template-columns:1fr; } }
  </style>
</head>
<body>
  <main>
    <header>
      <div><div class="eyebrow">HA NOVA</div><h1>Home Base</h1><p>Relay status and private local pairing.</p></div>
      <div class="pill ${statusClass}">${escapeHtml(model.health.ha_ws_connected ? "Ready" : "Needs attention")}</div>
    </header>
    <section class="grid" aria-label="Relay overview">
      <article class="card pairing">
        <div><div class="eyebrow">Pair this device</div><div class="code" aria-label="Pairing code ${escapeHtml(model.pairingCode)}">${escapeHtml(code)}</div><p>Enter this code when the HA NOVA setup asks for it.</p></div>
        <div><h2>Short-lived by design</h2><p>Single use. Expires in ${expiresMinutes} minute${expiresMinutes === 1 ? "" : "s"}. A successful pairing rotates it immediately.</p><p class="hint"><time datetime="${escapeHtml(new Date(model.pairingExpiresAtMs).toISOString())}">${escapeHtml(new Date(model.pairingExpiresAtMs).toLocaleString("en", { dateStyle: "medium", timeStyle: "short" }))}</time> · Refresh this page after expiry.</p></div>
      </article>
      <article class="card"><h2>Relay</h2><p>${escapeHtml(connection)}</p><dl><dt>Running version</dt><dd>${escapeHtml(model.health.version)}</dd><dt>Required floor</dt><dd>${escapeHtml(model.requiredRelayVersion)}</dd><dt>Uptime</dt><dd>${escapeHtml(formatUptime(model.health.uptime_s))}</dd></dl></article>
      <article class="card"><h2>Capabilities</h2><p>Effective settings reported by the Relay.</p><dl><dt>File access</dt><dd>${escapeHtml(model.health.file_access)}</dd><dt>Snapshots</dt><dd>${model.health.snapshots.files}</dd><dt>Snapshot size</dt><dd>${escapeHtml(formatBytes(model.health.snapshots.bytes))}</dd></dl></article>
      <article class="card install"><h2>Install HA NOVA</h2><p>Use the command for this stable Relay release, then return here for the pairing code.</p><div class="eyebrow">macOS / Linux</div><pre>${escapeHtml(commands.unix)}</pre><div class="eyebrow">Windows PowerShell</div><pre>${escapeHtml(commands.windows)}</pre></article>
    </section>
    <footer>Read-only status. No tracking, external assets, or Home Assistant mutations.</footer>
  </main>
</body>
</html>`;
}

function setHomeHeaders(response: ServerResponse): void {
  response.setHeader("cache-control", "no-store");
  response.setHeader("content-security-policy", HOME_CONTENT_SECURITY_POLICY);
  response.setHeader("x-content-type-options", "nosniff");
  response.setHeader("x-frame-options", "SAMEORIGIN");
  response.setHeader("referrer-policy", "no-referrer");
}

function singleHeader(request: IncomingMessage, name: string): string {
  const value = request.headers[name];
  return typeof value === "string" ? value.trim() : "";
}

function connectionText(health: HealthPayload): string {
  if (health.ha_ws_connected) return "Connected to Home Assistant";
  if (health.ha_ws_disconnect_reason === "auth") return "Home Assistant authentication failed";
  if (health.ha_ws_disconnect_reason === "network") return "Home Assistant is not reachable";
  return "Waiting for the first Home Assistant connection";
}

function installCommands(version: string): { unix: string; windows: string } {
  if (!/^\d+\.\d+\.\d+$/.test(version)) {
    const message = "Development build: use the install command from the latest GitHub release.";
    return { unix: message, windows: message };
  }
  const tag = `v${version}`;
  return {
    unix: `curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/${tag}/install.sh | HA_NOVA_VERSION=${tag} bash`,
    windows: `irm https://raw.githubusercontent.com/markusleben/ha-nova/${tag}/install.ps1 | iex`
  };
}

function formatUptime(seconds: number): string {
  const hours = Math.floor(seconds / 3_600);
  const minutes = Math.floor((seconds % 3_600) / 60);
  return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`;
}

function formatBytes(bytes: number): string {
  if (bytes < 1_024) return `${bytes} B`;
  if (bytes < 1_048_576) return `${(bytes / 1_024).toFixed(1)} KiB`;
  return `${(bytes / 1_048_576).toFixed(1)} MiB`;
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
  })[character]!);
}
