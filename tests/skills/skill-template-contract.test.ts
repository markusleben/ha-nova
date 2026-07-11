import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

// Skill Section Template v2 linter — enforces docs/reference/skill-architecture.md
// → "Skill Section Template (v2)". One behavior, one place: the English-only and
// word-budget checks moved here from ha-nova-contract.test.ts with dynamic
// globbing (hardcoded file lists silently exempt new skills).

const SKILLS_ROOT = "skills";

const SUBSKILLS = readdirSync(SKILLS_ROOT).filter(
  (d) => d !== "ha-nova" && statSync(join(SKILLS_ROOT, d)).isDirectory(),
);

const ALL_SKILL_MD_FILES = ((): string[] => {
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry);
      if (statSync(p).isDirectory()) walk(p);
      else if (entry.endsWith(".md")) files.push(p);
    }
  };
  walk(SKILLS_ROOT);
  return files;
})();

// Canonical H2 relative order; "Bootstrap" matches all declared variants.
const CANON = [
  "Scope",
  "Bootstrap",
  "Relay Contract",
  "Flow",
  "Error Handling",
  "Output Format",
  "Safety",
  "Guardrails",
  "References",
];

const BOOTSTRAP_HEADINGS: Record<string, string> = {
  onboarding: "Bootstrap",
  fallback: "Bootstrap (only before Relay-Ready calls)",
};
const DEFAULT_BOOTSTRAP = "Bootstrap (once per session)";

// The diagnostics skill's whole body is remediation commands — declared
// exception, see skill-architecture.md → Declared deviations.
const RELAY_CONTRACT_EXEMPT = new Set(["onboarding"]);

const REFERENCES_REQUIRED = new Set(["write", "helper"]);

const FORBIDDEN_HEADINGS = [
  "Output Rules",
  "Safety Baseline",
  "Safety Guardrails",
  "Agent Flow",
];

// Word budgets: default 1000. Documented ratchets for content-dense skills;
// write and review drop again after the masterplan A5 token diet (References
// split / checks.md self-containment).
const WORD_BUDGETS: Record<string, number> = {
  write: 1200,
  scene: 1250,
  "service-call": 1100,
  health: 1150,
  fallback: 2050,
  helper: 3600,
  review: 4800,
};
const DEFAULT_WORD_BUDGET = 1000;

// Internal review-check codes (S-01, R-18, H-09, ...) may flow only between
// the reviewer/mutation files that implement the dedup logic; user-flow
// skills must never carry them (output-rules.md forbids surfacing them).
const CHECK_CODE_ALLOWLIST = new Set([
  "skills/review/checks.md",
  "skills/review/SKILL.md",
  "skills/write/SKILL.md",
  "skills/helper/SKILL.md",
  "skills/ha-nova/output-rules.md",
  "skills/ha-nova/write-safety.md",
  "skills/ha-nova/automation-patterns.md",
  "skills/ha-nova/payload-schemas.md",
  "skills/ha-nova/safe-refactoring.md",
  "skills/ha-nova/template-guidelines.md",
  "skills/ha-nova/best-practices.md",
]);

const GERMAN_PATTERNS = [
  /\bAnalysiere\b/,
  /\bZeige\b/,
  /\bErstelle\b/,
  /\bÄndere\b/,
  /\bSpeichere\b/,
  /\bImportiere\b/,
  /\bmeine\b/,
  /\bfehlt\b/,
  /\blöschen\b/,
  /\bBitte\b/,
  /\bBedingung\b/,
  /\bWohnzimmer\b/,
  /\bfunktioniert\b/,
  /\bgeht nicht\b/,
  /\bist falsch\b/,
];

function stripFences(md: string): string {
  return md.replace(/```[\s\S]*?```/g, "");
}

function stripInlineCode(md: string): string {
  return md.replace(/`[^`\n]*`/g, "");
}

function parseFrontmatter(md: string): Record<string, string> {
  const match = md.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  const fm: Record<string, string> = {};
  for (const line of (match[1] ?? "").split("\n")) {
    const idx = line.indexOf(":");
    if (idx > 0) fm[line.slice(0, idx).trim()] = line.slice(idx + 1).trim();
  }
  return fm;
}

function h2s(md: string): string[] {
  return [...stripFences(md).matchAll(/^## (.+)$/gm)].map((m) => (m[1] ?? "").trim());
}

function sectionBody(md: string, heading: string): string {
  const stripped = stripFences(md);
  const re = new RegExp(`^## ${heading.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}$`, "m");
  const start = stripped.search(re);
  if (start < 0) return "";
  const rest = stripped.slice(start).split("\n").slice(1);
  const end = rest.findIndex((l) => l.startsWith("## "));
  return (end < 0 ? rest : rest.slice(0, end)).join("\n");
}

function subskillPath(name: string): string {
  return join(SKILLS_ROOT, name, "SKILL.md");
}

describe("skill template v2 contract", () => {
  it("keeps every sub-skill frontmatter spec-compliant", () => {
    for (const name of SUBSKILLS) {
      const fm = parseFrontmatter(readFileSync(subskillPath(name), "utf8"));
      const fmName = fm.name ?? "";
      const fmDescription = fm.description ?? "";
      expect(fmName, `${name}: frontmatter name must equal directory name`).toBe(name);
      expect(fmName, `${name}: lowercase-hyphen name`).toMatch(/^[a-z][a-z0-9-]*$/);
      // 'ha-nova-' namespacing on flat installs must stay under the spec's 64.
      expect(fmName.length, `${name}: name too long for namespaced installs`).toBeLessThanOrEqual(56);
      expect(fmDescription, `${name}: description required`).toBeTruthy();
      expect(fmDescription.length, `${name}: description over spec limit`).toBeLessThanOrEqual(1024);
      const allowed = new Set(["name", "description", "license", "compatibility"]);
      for (const key of Object.keys(fm)) {
        expect(allowed.has(key), `${name}: frontmatter key '${key}' not allowed`).toBe(true);
      }
    }
  });

  it("pins the fallback discovery-time write gate in its description", () => {
    const fm = parseFrontmatter(readFileSync(subskillPath("fallback"), "utf8"));
    expect(fm.description).toContain(
      "Must be invoked before any raw relay write operation",
    );
  });

  it("keeps required sections present per skill class", () => {
    for (const name of SUBSKILLS) {
      const headings = h2s(readFileSync(subskillPath(name), "utf8"));
      const bootstrap = BOOTSTRAP_HEADINGS[name] ?? DEFAULT_BOOTSTRAP;
      const required = ["Scope", bootstrap, "Flow", "Output Format", "Safety"];
      if (!RELAY_CONTRACT_EXEMPT.has(name)) required.push("Relay Contract");
      if (REFERENCES_REQUIRED.has(name)) required.push("References");
      for (const section of required) {
        expect(headings, `${name}: missing required section '## ${section}'`).toContain(section);
      }
    }
  });

  it("keeps canonical sections in canonical order", () => {
    for (const name of SUBSKILLS) {
      const headings = h2s(readFileSync(subskillPath(name), "utf8"));
      const canonical = headings
        .map((h) => (h.startsWith("Bootstrap") ? "Bootstrap" : h))
        .filter((h) => CANON.includes(h));
      const indexes = canonical.map((h) => CANON.indexOf(h));
      for (let i = 1; i < indexes.length; i++) {
        expect(
          indexes[i] ?? -1,
          `${name}: '## ${canonical[i]}' must come after '## ${canonical[i - 1]}'`,
        ).toBeGreaterThan(indexes[i - 1] ?? -1);
      }
    }
  });

  it("keeps Error Handling directly before Output Format when present", () => {
    for (const name of SUBSKILLS) {
      const headings = h2s(readFileSync(subskillPath(name), "utf8"));
      const idx = headings.indexOf("Error Handling");
      if (idx < 0) continue;
      expect(
        headings[idx + 1],
        `${name}: the H2 after '## Error Handling' must be '## Output Format'`,
      ).toBe("Output Format");
    }
  });

  it("rejects forbidden heading variants in sub-skills", () => {
    for (const name of SUBSKILLS) {
      const headings = h2s(readFileSync(subskillPath(name), "utf8"));
      for (const bad of FORBIDDEN_HEADINGS) {
        expect(headings, `${name}: use the canonical heading, not '## ${bad}'`).not.toContain(bad);
      }
      const bootstrapVariants = headings.filter((h) => h.startsWith("Bootstrap"));
      const expected = BOOTSTRAP_HEADINGS[name] ?? DEFAULT_BOOTSTRAP;
      expect(
        bootstrapVariants,
        `${name}: Bootstrap heading must be exactly '## ${expected}'`,
      ).toEqual([expected]);
    }
  });

  it("starts every Output Format section with the shared output-rules pointer", () => {
    for (const name of SUBSKILLS) {
      const body = sectionBody(readFileSync(subskillPath(name), "utf8"), "Output Format");
      const firstLine = body.split("\n").find((l) => l.trim() !== "") ?? "";
      expect(
        firstLine.trimStart().startsWith("Apply `skills/ha-nova/output-rules.md`"),
        `${name}: Output Format must start with the output-rules pointer, got: ${firstLine}`,
      ).toBe(true);
    }
  });

  it("uses App terminology in prose across all skill files", () => {
    for (const file of ALL_SKILL_MD_FILES) {
      const prose = stripInlineCode(stripFences(readFileSync(file, "utf8")));
      const match = prose.match(/\badd-?ons?\b/i);
      expect(
        match,
        `${file}: prose says 'App(s)'; 'add-on' is allowed only as a code literal (found '${match?.[0]}')`,
      ).toBeNull();
    }
  });

  it("keeps internal review-check codes out of user-flow skills", () => {
    for (const file of ALL_SKILL_MD_FILES) {
      if (CHECK_CODE_ALLOWLIST.has(file.split("\\").join("/"))) continue;
      const content = readFileSync(file, "utf8");
      const match = content.match(/\b[SRPMFH]-\d{2}\b/);
      expect(
        match,
        `${file}: internal check code '${match?.[0]}' outside the reviewer allowlist`,
      ).toBeNull();
    }
  });

  it("enforces English-only content across all skill files", () => {
    for (const file of ALL_SKILL_MD_FILES) {
      const content = readFileSync(file, "utf8");
      for (const pattern of GERMAN_PATTERNS) {
        expect(
          pattern.test(content),
          `${file} contains German text matching ${pattern}`,
        ).toBe(false);
      }
    }
  });

  it("keeps every sub-skill within its word budget", () => {
    for (const name of SUBSKILLS) {
      const content = readFileSync(subskillPath(name), "utf8");
      const wordCount = content.trim().split(/\s+/).length;
      const limit = WORD_BUDGETS[name] ?? DEFAULT_WORD_BUDGET;
      expect(
        wordCount,
        `skills/${name}/SKILL.md has ${wordCount} words (limit ${limit})`,
      ).toBeLessThan(limit);
    }
  });
});
