import { spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

const script = "scripts/release/provision-macos-signing-secrets.sh";
const temporaryRoots: string[] = [];

function executable(path: string, body: string): void {
  writeFileSync(path, body, { mode: 0o700 });
  chmodSync(path, 0o700);
}

function runProvision(options: {
  activeUser?: string;
  identity?: string;
  opensslVersion?: string;
  password?: string;
}) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-macos-provision-"));
  temporaryRoots.push(root);
  const bin = join(root, "bin");
  const p12 = join(root, "developer-id.p12");
  const trace = join(root, "gh.trace");
  const opensslTrace = join(root, "openssl.trace");

  executable(
    join(root, "make-bin"),
    `#!/usr/bin/env bash
set -euo pipefail
mkdir -p "$1"
`,
  );
  spawnSync(join(root, "make-bin"), [bin], { encoding: "utf8" });
  writeFileSync(p12, "encrypted-p12-fixture", { mode: 0o600 });

  executable(
    join(bin, "gh"),
    `#!/usr/bin/env bash
set -euo pipefail
case "\${1:-} \${2:-}" in
  "auth status")
    exit 0
    ;;
  "api user")
    printf '%s\\n' "\${FAKE_GITHUB_USER}"
    ;;
  "api repos/markusleben/ha-nova/environments/production")
    exit 0
    ;;
  "repo view")
    printf '%s\\n' "markusleben/ha-nova"
    ;;
  "secret set")
    value="$(cat)"
    printf '%s\\t%s\\n' "\${3:-}" "\${#value}" >>"\${FAKE_GH_TRACE}"
    ;;
  *)
    printf 'unexpected gh call: %s\\n' "$*" >&2
    exit 91
    ;;
esac
`,
  );
  executable(
    join(bin, "openssl"),
    `#!/usr/bin/env bash
set -euo pipefail
case "\${1:-}" in
  version)
    printf '%s\\n' "\${FAKE_OPENSSL_VERSION}"
    ;;
  pkcs12)
    printf '%s\\n' "$*" >>"\${FAKE_OPENSSL_TRACE}"
    IFS= read -r supplied_password <&3
    [[ "\${supplied_password}" == "correct-password" ]] || exit 2
    if [[ "$*" == *"-nocerts"* ]]; then
      printf '%s\\n' "fixture-private-key"
    else
      printf '%s\\n' "fixture-certificate"
    fi
    ;;
  pkey)
    cat >/dev/null
    ;;
  x509)
    cat >/dev/null
    printf 'subject=CN = %s\\n' "\${FAKE_CERTIFICATE_IDENTITY}"
    ;;
  *)
    exit 92
    ;;
esac
`,
  );

  const result = spawnSync("bash", [script, p12], {
    encoding: "utf8",
    env: {
      ...process.env,
      FAKE_CERTIFICATE_IDENTITY:
        options.identity ??
        "Developer ID Application: Markus Leben (CTF9J94274)",
      FAKE_GH_TRACE: trace,
      FAKE_GITHUB_USER: options.activeUser ?? "markusleben",
      FAKE_OPENSSL_TRACE: opensslTrace,
      FAKE_OPENSSL_VERSION:
        options.opensslVersion ?? "OpenSSL 3.4.1 11 Feb 2025",
      PATH: `${bin}${delimiter}${process.env.PATH ?? ""}`,
    },
    input: `${options.password ?? "correct-password"}\n`,
  });
  return {
    ...result,
    opensslTrace: readFileSync(opensslTrace, {
      encoding: "utf8",
      flag: "a+",
    }),
    trace: readFileSync(trace, { encoding: "utf8", flag: "a+" }),
  };
}

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) {
    rmSync(root, { force: true, recursive: true });
  }
});

describe("macOS signing secret provisioning", () => {
  it("requests one hidden password and uploads only the two protected secrets", () => {
    const result = runProvision({});
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(
      result.stderr.match(/password \(input hidden; requested once\)/g),
    ).toHaveLength(1);
    expect(result.stderr).not.toContain("correct-password");
    expect(result.stdout).not.toContain("correct-password");
    expect(
      result.trace
        .trim()
        .split("\n")
        .map((line) => line.split("\t")[0]),
    ).toEqual([
      "HA_NOVA_MACOS_CERTIFICATE_P12_BASE64",
      "HA_NOVA_MACOS_CERTIFICATE_PASSWORD",
    ]);
  });

  it.each([
    ["OpenSSL 3", "OpenSSL 3.4.1 11 Feb 2025", true],
    ["LibreSSL", "LibreSSL 3.3.6", false],
  ])("uses the compatible Apple PKCS#12 mode with %s", (_name, version, legacy) => {
    const result = runProvision({ opensslVersion: version });
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    const calls = result.opensslTrace.trim().split("\n");
    expect(calls).toHaveLength(2);
    for (const call of calls) {
      expect(call.includes("-legacy")).toBe(legacy);
    }
  });

  it("fails before prompting or uploading under the wrong GitHub account", () => {
    const result = runProvision({ activeUser: "another-user" });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "active GitHub user must be markusleben, got another-user",
    );
    expect(result.stderr).not.toContain("requested once");
    expect(result.trace).toBe("");
  });

  it("rejects an unexpected signing identity before uploading", () => {
    const result = runProvision({
      identity: "Developer ID Application: Someone Else (WRONGTEAM)",
    });
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "does not contain the expected Developer ID identity",
    );
    expect(result.trace).toBe("");
  });

  it("rejects a wrong password after one prompt and before uploading", () => {
    const result = runProvision({ password: "wrong-password" });
    expect(result.status).not.toBe(0);
    expect(
      result.stderr.match(/password \(input hidden; requested once\)/g),
    ).toHaveLength(1);
    expect(result.stderr).toContain(
      "password is incorrect or its private key is invalid",
    );
    expect(result.stderr).not.toContain("wrong-password");
    expect(result.trace).toBe("");
  });
});
