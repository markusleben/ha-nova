import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("project docs contract", () => {
  const project = readFileSync("PROJECT.md", "utf8");
  const support = readFileSync("SUPPORT.md", "utf8");
  const novaReadme = readFileSync("nova/README.md", "utf8");
  const governance = readFileSync("docs/reference/documentation-governance.md", "utf8");
  const breadcrumbs = readFileSync("docs/breadcrumbs.md", "utf8");

  it("keeps PROJECT.md scoped to internal product context instead of public install truth", () => {
    expect(project).toContain("This file is internal product context only.");
    expect(project).toContain("Public install, support, and platform truth lives in `README.md`.");
    expect(project).toContain("Contributor workflow lives in `CONTRIBUTING.md`.");
    expect(project).toContain("Release/runbook truth lives in `docs/releasing.md`.");
    expect(project).not.toContain("Current release-facing support matrix:");
    expect(project).not.toContain("Fetch and follow instructions from https://raw.githubusercontent.com/markusleben/ha-nova/main/.codex/INSTALL.md");
  });

  it("tracks the active architecture surfaces and current skill inventory", () => {
    expect(project).toContain("fallback");
    expect(project).toContain("automation-patterns.md");
    expect(project).toContain("documentation-governance.md");
    expect(project).not.toContain("## Active Documentation");
    expect(project).not.toContain("## Current Product Surfaces");
  });

  it("scopes the English-only policy to skills and skill-like source docs", () => {
    expect(project).toContain("skills and skill-like source docs stay English-only");
    expect(project).not.toContain("English-only across the whole project");
  });

  it("keeps SUPPORT.md as a thin routing page", () => {
    expect(support).toContain("Run `ha-nova doctor` first.");
    expect(support).toContain("Then use the right channel:");
    expect(support).toContain("follow `SECURITY.md`");
    expect(support).not.toContain("## Before Opening an Issue");
  });

  it("keeps nova/README.md as a pointer instead of a second relay truth surface", () => {
    expect(novaReadme).toContain("Use:");
    expect(novaReadme).toContain("`README.md` for the public product/install/support view");
    expect(novaReadme).toContain("`nova/DOCS.md` for Home Assistant App / relay setup");
    expect(novaReadme).toContain("intentionally only a pointer");
    expect(novaReadme).not.toContain("Persistent WebSocket connection");
  });

  it("treats superpowers docs as archive-only history", () => {
    expect(governance).toContain("`docs/archive/superpowers/` contains historical superpowers plans/specs only");
    expect(governance).toContain("do not create new active docs under `docs/archive/superpowers/`");
    expect(governance).not.toContain("do not create new active docs under `docs/superpowers/`");
  });

  it("defines a canonical active work-doc path and keeps breadcrumbs short/current", () => {
    expect(governance).toContain("keep active work docs under `docs/work/`");
    expect(governance).toContain("`docs/work/`");
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.gemini/INSTALL.md`, `.opencode/INSTALL.md`");
    expect(breadcrumbs).toContain("Historical breadcrumb log:");
    expect(breadcrumbs).toContain("`docs/archive/breadcrumbs.md`");
    expect(breadcrumbs).not.toContain("docs/superpowers/");
  });
});
