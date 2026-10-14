import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import { expect, it } from "vitest";

export function registerWorkflowPinsContractTests(): void {
  it("pins every action in every workflow and scans the Go CLI fail-closed", () => {
    // A moved upstream tag would run under the job's token (packages: write in
    // relay-image.yml), so every `uses:` line must be an immutable SHA with its
    // exact release tag.
    const exactAction =
      /^\s+(?:-\s+)?uses:\s+[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+(?:\/[A-Za-z0-9_.-]+)*@[0-9a-f]{40}\s+#\s+v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\s*$/;
    const workflowDir = ".github/workflows";
    const workflows = readdirSync(workflowDir).filter((name) => /\.ya?ml$/.test(name));
    expect(workflows.length).toBeGreaterThan(5);
    for (const name of workflows) {
      const lines = readFileSync(join(workflowDir, name), "utf8").split(/\r?\n/);
      lines.forEach((line, index) => {
        if (line.trimStart().startsWith("#")) {
          return;
        }
        // Flow/anchor/merge-key step syntax could hide a `uses:` from a line matcher.
        expect(line, `${name}:${index + 1} must use canonical block step syntax`).not.toMatch(
          /^\s+(?:-\s*(?:[&*{]|\?)|<<\s*:)/,
        );
        if (!/^\s*(?:-\s+)?["']?uses["']?\s*:/.test(line)) {
          return;
        }
        expect(line, `${name}:${index + 1} must pin its action to a full commit SHA with an exact vX.Y.Z comment`).toMatch(exactAction);
      });
    }

    // CodeQL scans the Go CLI in the same `analyze` job (a matrix would rename
    // the required check), and analyzer failure is not swallowed.
    const codeql = readFileSync(join(workflowDir, "codeql.yml"), "utf8");
    expect(codeql).toContain("\n  analyze:\n");
    expect(codeql).toContain("languages: go, javascript-typescript");
    expect(codeql).not.toContain("strategy:");
    expect(codeql).not.toContain("continue-on-error");
    expect(codeql.indexOf("go-version-file: cli/go.mod")).toBeGreaterThan(0);
    expect(codeql.indexOf("go-version-file: cli/go.mod")).toBeLessThan(codeql.indexOf("github/codeql-action/init@"));
  });
}
