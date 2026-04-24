import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("project docs contract", () => {
  const project = readFileSync("PROJECT.md", "utf8");
  const support = readFileSync("SUPPORT.md", "utf8");
  const codeOfConduct = readFileSync("CODE_OF_CONDUCT.md", "utf8");
  const novaReadme = readFileSync("nova/README.md", "utf8");
  const novaDocs = readFileSync("nova/DOCS.md", "utf8");
  const bridgeArchitecture = readFileSync("docs/reference/bridge-architecture.md", "utf8");
  const governance = readFileSync("docs/reference/documentation-governance.md", "utf8");
  const hermesValidation = readFileSync("docs/reference/hermes-platform-validation.md", "utf8");
  const hermesValidationIssue = readFileSync(".github/ISSUE_TEMPLATE/hermes-platform-validation.yml", "utf8");

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
    expect(support).toContain("contact the maintainer privately via GitHub:");
    expect(support).toContain("`https://github.com/markusleben`");
    expect(support).toContain("Do not post conduct reports in public issues or pull requests.");
    expect(support).not.toContain("## Before Opening an Issue");
  });

  it("keeps conduct reporting on a real private path", () => {
    expect(codeOfConduct).toContain("Report conduct incidents privately to the maintainer");
    expect(codeOfConduct).toContain("GitHub profile/contact path");
    expect(codeOfConduct).toContain("`https://github.com/markusleben`");
    expect(codeOfConduct).toContain("Do not report conduct incidents in public issues or pull requests.");
    expect(codeOfConduct).not.toContain("private GitHub issue");
    expect(support).not.toContain("private GitHub issue");
  });

  it("keeps nova/README.md as a pointer instead of a second relay truth surface", () => {
    expect(novaReadme).toContain("Use:");
    expect(novaReadme).toContain("`README.md` for the public product/install/support view");
    expect(novaReadme).toContain("`nova/DOCS.md` for Home Assistant App / relay setup");
    expect(novaReadme).toContain("intentionally only a pointer");
    expect(novaReadme).not.toContain("Persistent WebSocket connection");
  });

  it("keeps nova/DOCS.md aligned with the relay architecture reference", () => {
    expect(bridgeArchitecture).toContain("Requires the relay bearer token like the other implemented endpoints.");
    expect(bridgeArchitecture).toContain("Wrapped in the standard envelope");
    expect(novaDocs).toContain("including `GET /health`");
    expect(novaDocs).toContain("Authorization: Bearer <token>");
    expect(novaDocs).toContain("[latest GitHub release](https://github.com/markusleben/ha-nova/releases/latest)");
    expect(novaDocs).not.toContain("raw.githubusercontent.com/markusleben/ha-nova/main/install.sh");
    expect(novaDocs).toContain('"ok": true');
    expect(novaDocs).toContain('"data": {');
  });

  it("treats superpowers docs as archive-only history", () => {
    expect(governance).toContain("`docs/archive/superpowers/` contains historical superpowers plans/specs only");
    expect(governance).toContain("do not create new active docs under `docs/archive/superpowers/`");
    expect(governance).not.toContain("do not create new active docs under `docs/superpowers/`");
  });

  it("defines a canonical active work-doc path and keeps breadcrumbs short/current", () => {
    expect(governance).toContain("keep active work docs under `docs/work/`");
    expect(governance).toContain("`docs/work/`");
    expect(governance).toContain("`.claude/INSTALL.md`, `.codex/INSTALL.md`, `.gemini/INSTALL.md`, `.opencode/INSTALL.md`, `.hermes/INSTALL.md`");
    expect(governance).toContain("`SUPPORT.md`");
    expect(governance).toContain("`CODE_OF_CONDUCT.md`");
    expect(governance).toContain("`nova/README.md`");
    expect(governance).toContain("`docs/archive/breadcrumbs.md` is the long historical breadcrumb ledger");
    expect(governance).toContain("keep the root `docs/breadcrumbs.md` short and current only");
  });

  it("tracks Hermes support evidence in a dedicated active reference doc", () => {
    expect(hermesValidation).toContain("This is the active truth source for Hermes support evidence in HA NOVA.");
    expect(hermesValidation).toContain("## Support Status");
    expect(hermesValidation).toContain("Supported with limitation");
    expect(hermesValidation).toContain("Not supported");
    expect(hermesValidation).toContain("## Evidence Status");
    expect(hermesValidation).toContain("Maintainer-validated");
    expect(hermesValidation).toContain("Community validation");
    expect(hermesValidation).toContain("Planned / not yet validated");
    expect(hermesValidation).toContain("## Repair Check");
    expect(hermesValidation).toContain("configured but not attached");
    expect(hermesValidation).toContain("Hermes Agent ready now");
    expect(hermesValidation).toContain("GNOME Keyring");
    expect(hermesValidation).toContain("WSL2");
    expect(hermesValidation).toContain("Native Windows Hermes is not part of the HA NOVA support model.");
  });

  it("keeps community validation intake structured and privacy-safe", () => {
    expect(hermesValidationIssue).toContain("Hermes platform validation");
    expect(hermesValidationIssue).toContain("never paste Home Assistant long-lived access tokens");
    expect(hermesValidationIssue).toContain("never paste relay auth tokens");
    expect(hermesValidationIssue).toContain("never paste local keyring passwords");
    expect(hermesValidationIssue).toContain("Windows native (unsupported today; use WSL2 for the supported path)");
    expect(hermesValidationIssue).toContain("Install source");
    expect(hermesValidationIssue).toContain("Host form");
    expect(hermesValidationIssue).toContain("Helper script or `HA_NOVA_*` overrides used");
    expect(hermesValidationIssue).toContain("Entry point used");
    expect(hermesValidationIssue).toContain("Redact hostnames, LAN IPs, private paths, and secrets");
    expect(hermesValidationIssue).toContain("hermes skills list");
    expect(hermesValidationIssue).toContain("ha-nova*");
    expect(hermesValidationIssue).toContain("Optional redacted `ha-nova doctor` output");
    expect(hermesValidationIssue).toContain("session");
    expect(hermesValidationIssue).toContain("Secret Service backend");
  });
});
