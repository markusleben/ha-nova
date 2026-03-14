/**
 * Shared test helpers for onboarding integration tests.
 * Provides isolated HOME dirs and mock binaries for CI-safe testing.
 */
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const FIXTURES_DIR = resolve(__dirname, "../fixtures");
const REPO_ROOT = resolve(__dirname, "../..");

export { REPO_ROOT };

function stripGitEnv(env: NodeJS.ProcessEnv): Record<string, string> {
  const cleanEnv: Record<string, string> = {};

  for (const [key, value] of Object.entries(env)) {
    if (key.startsWith("GIT_")) {
      continue;
    }
    if (value === undefined) {
      continue;
    }
    cleanEnv[key] = value;
  }

  return cleanEnv;
}

export interface MockHomeOpts {
  /** Pre-populate config file with these values */
  config?: { HA_HOST: string; HA_URL: string; RELAY_BASE_URL: string };
  /** Create a fake keychain token file (since we mock `security`) */
  keychainToken?: string;
  /** Pre-install skills for this client */
  skills?: "codex" | "gemini" | "opencode" | "claude" | "all";
}

/**
 * Creates an isolated HOME directory with optional pre-populated config/token/skills.
 */
export function createMockHome(opts: MockHomeOpts = {}): string {
  const home = mkdtempSync(join(tmpdir(), "ha-nova-test-"));

  if (opts.config) {
    const configDir = join(home, ".config/ha-nova");
    mkdirSync(configDir, { recursive: true });
    // Match persist_config format: printf '%q' produces shell-quoted values.
    // For simple values (IPs, URLs without special chars) quoting is a no-op,
    // but we wrap in single quotes to mirror the %q output for consistency.
    const esc = (v: string) => v.replace(/'/g, "'\\''");
    const lines = [
      `HA_HOST='${esc(opts.config.HA_HOST)}'`,
      `HA_URL='${esc(opts.config.HA_URL)}'`,
      `RELAY_BASE_URL='${esc(opts.config.RELAY_BASE_URL)}'`,
    ];
    writeFileSync(join(configDir, "onboarding.env"), lines.join("\n") + "\n", { mode: 0o600 });
  }

  if (opts.keychainToken) {
    // Mock security binary reads from this file
    const configDir = join(home, ".config/ha-nova");
    mkdirSync(configDir, { recursive: true });
    writeFileSync(join(configDir, ".mock-keychain-token"), opts.keychainToken, { mode: 0o600 });
  }

  return home;
}

export interface MockBinaryOpts {
  /** Fixture file to serve for /health requests (default: relay-health-ok.json) */
  healthFixture?: string;
  /** Fixture file to serve for /ws requests (default: relay-ws-pong.json) */
  wsFixture?: string;
  /** HTTP status code to serve for /ws requests (default: 200) */
  wsStatusCode?: number;
  /** Fixture file to serve for /api/discovery_info (default: ha-discovery-info.json) */
  discoveryFixture?: string;
  /** Make curl fail (simulates unreachable relay) */
  curlFails?: boolean;
  /** Custom security mock behavior */
  securityToken?: string;
}

/**
 * Creates mock binaries (security, curl, openssl) in a temp bin dir.
 * Returns the bin dir path to prepend to PATH.
 */
export function createMockBinaries(opts: MockBinaryOpts = {}): string {
  const binDir = mkdtempSync(join(tmpdir(), "ha-nova-bin-"));

  // --- security mock (Keychain) ---
  const securityScript = `#!/usr/bin/env bash
case "$1" in
  find-generic-password)
    # Check for mock keychain token file
    token_file="\${HOME}/.config/ha-nova/.mock-keychain-token"
    if [[ -f "$token_file" ]]; then
      cat "$token_file"
    fi
    exit 0
    ;;
  add-generic-password|delete-generic-password)
    exit 0
    ;;
esac
exit 0
`;
  writeFileSync(join(binDir, "security"), securityScript, { mode: 0o755 });

  // --- curl mock (fixture router) ---
  const healthFixture = join(FIXTURES_DIR, opts.healthFixture ?? "relay-health-ok.json");
  const wsFixture = join(FIXTURES_DIR, opts.wsFixture ?? "relay-ws-pong.json");
  const wsStatusCode = String(opts.wsStatusCode ?? 200);
  const discoveryFixture = join(FIXTURES_DIR, opts.discoveryFixture ?? "ha-discovery-info.json");

  const curlScript = opts.curlFails
    ? `#!/usr/bin/env bash\nexit 1\n`
    : `#!/usr/bin/env bash
# Fixture-based curl mock router
outfile=""
headers_file=""
write_code=""
url=""

# Parse args — collect last non-flag argument as URL
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -o) outfile="$2"; shift 2 ;;
    -D) headers_file="$2"; shift 2 ;;
    -w) write_code="$2"; shift 2 ;;
    -H|-d) shift 2 ;;
    -sS|-s|-S) shift ;;
    --connect-timeout|--max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done

serve_fixture() {
  local fixture="$1"
  if [[ -n "$outfile" ]]; then
    cat "$fixture" > "$outfile"
  else
    cat "$fixture"
  fi
  if [[ -n "$headers_file" ]]; then
    printf 'content-type: application/json\\n' > "$headers_file"
  fi
  if [[ -n "$write_code" ]]; then
    printf '200'
  fi
  exit 0
}

case "$url" in
  */health) serve_fixture "${healthFixture}" ;;
  */ws)
    if [[ -n "$outfile" ]]; then
      cat "${wsFixture}" > "$outfile"
    else
      cat "${wsFixture}"
    fi
    if [[ -n "$headers_file" ]]; then
      printf 'content-type: application/json\\n' > "$headers_file"
    fi
    if [[ -n "$write_code" ]]; then
      printf '${wsStatusCode}'
    fi
    exit 0
    ;;
  */api/discovery_info) serve_fixture "${discoveryFixture}" ;;
  */manifest.json)
    # Not found — let HA probing continue
    if [[ -n "$write_code" ]]; then printf '404'; fi
    exit 1
    ;;
  *raw.githubusercontent.com*)
    # Remote version check — serve local version.json
    serve_fixture "${REPO_ROOT}/version.json"
    ;;
  *github.com/*/releases/download/*)
    # Relay binary download — serve a dummy executable script
    dummy='#!/usr/bin/env bash\necho "mock-relay"\n'
    if [[ -n "$outfile" ]]; then
      printf "$dummy" > "$outfile"
    else
      printf "$dummy"
    fi
    if [[ -n "$write_code" ]]; then printf '200'; fi
    exit 0
    ;;
  *)
    if [[ -n "$write_code" ]]; then printf '000'; fi
    exit 1
    ;;
esac
`;
  writeFileSync(join(binDir, "curl"), curlScript, { mode: 0o755 });

  // --- openssl mock ---
  writeFileSync(
    join(binDir, "openssl"),
    "#!/usr/bin/env bash\necho deadbeefdeadbeefdeadbeefdeadbeef\n",
    { mode: 0o755 },
  );

  // --- open mock (no-op browser) ---
  writeFileSync(join(binDir, "open"), "#!/usr/bin/env bash\nexit 0\n", { mode: 0o755 });

  // --- claude mock (fast no-op plugin CLI) ---
  writeFileSync(
    join(binDir, "claude"),
    `#!/usr/bin/env bash
if [[ "$1" == "plugin" ]]; then
  exit 0
fi
exit 0
`,
    { mode: 0o755 },
  );

  return binDir;
}

/** Read a fixture file as string */
export function readFixture(name: string): string {
  return readFileSync(join(FIXTURES_DIR, name), "utf8");
}

/** Build PATH with mock bin dir prepended */
export function mockEnv(
  home: string,
  binDir: string,
  extra: Record<string, string> = {},
): Record<string, string> {
  const env = stripGitEnv(process.env);

  return {
    ...env,
    HOME: home,
    PATH: `${binDir}:${process.env.PATH ?? ""}`,
    ...extra,
  };
}

export function mockEnvWithBase(
  baseEnv: NodeJS.ProcessEnv,
  extra: Record<string, string> = {},
): Record<string, string> {
  return {
    ...stripGitEnv(baseEnv),
    ...extra,
  };
}
