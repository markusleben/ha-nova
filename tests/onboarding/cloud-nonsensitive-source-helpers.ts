import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

export const NONSENSITIVE_SCRIPT = resolve(
  "scripts/release/verify-cloud-nonsensitive-source.mjs",
);

export type NonsensitiveFixture = {
  root: string;
  base: string;
};

export function git(root: string, args: string[]): string {
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

export function write(
  root: string,
  relative: string,
  content: string | Buffer,
): void {
  const target = join(root, relative);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, content);
}

export function commitAll(root: string, message: string): string {
  git(root, ["add", "-A"]);
  git(root, ["commit", "-qm", message]);
  return git(root, ["rev-parse", "HEAD"]);
}

export function fixture(): NonsensitiveFixture {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-nonsensitive-"));
  git(root, ["init", "-q"]);
  write(
    root,
    "README.md",
    "# HA NOVA\n\ncurl -fsSL https://example/install.sh | bash\n",
  );
  write(root, "AGENTS.md", "# Agent policy\n");
  write(root, "docs/reference/a.md", "reference\n");
  write(root, "skills/write/SKILL.md", "skill\n");
  write(root, "tests/sample.test.ts", "export {};\n");
  write(root, "scripts/release/tool.sh", "#!/usr/bin/env bash\n");
  const base = commitAll(root, "base");
  return { root, base };
}

export function run(root: string, base: string, target: string) {
  return spawnSync("node", [NONSENSITIVE_SCRIPT, root, base, target], {
    encoding: "utf8",
  });
}
