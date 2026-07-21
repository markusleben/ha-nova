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

const MUTATION_SKILLS = new Set([
  "write",
  "diagnose",
  "media",
  "notify",
  "camera",
  "mqtt",
  "yaml-config",
  "assist",
  "admin",
  "helper",
  "dashboard",
  "scene",
  "organize",
  "todo",
  "backup",
  "updates",
  "energy",
  "maintenance",
  "service-call",
  "integration-setup",
  "calendar",
  "fallback",
  "review",
]);
const READ_ONLY_SKILLS = new Set([
  "read",
  "external-sources",
  "entity-discovery",
  "history",
  "health",
  "onboarding",
]);

// SSOT: the fenced blocks in skill-architecture.md → "Safety Core (canonical text)".
const SAFETY_CORE_BLOCKS = ((): { mutation: string; readOnly: string } => {
  const doc = readFileSync("docs/reference/skill-architecture.md", "utf8");
  const section = doc.split("### Safety Core (canonical text)")[1] ?? "";
  const fenced = [...section.matchAll(/```text\n([\s\S]*?)```/g)].map((m) =>
    (m[1] ?? "").trimEnd(),
  );
  return { mutation: fenced[0] ?? "", readOnly: fenced[1] ?? "" };
})();

// Word budgets: default 1150 (every sub-skill carries the mandatory ~100-word
// Safety Core, the output-rules pointer, and the A6 frontmatter). Documented
// ratchets for content-dense skills. Note: the A5 token diet cut the
// TRANSITIVE load (lazy references), not these file sizes — write carries the
// on-demand trigger list itself.
const WORD_BUDGETS: Record<string, number> = {
  // pre-save snapshot capture + snapshot recovery guidance (Wave 2).
  energy: 1250,
  // consumer checks before area delete/rename/disable (Wave 1b) + metadata
  // snapshot capture (Wave 2).
  organize: 1250,
  // write/mqtt ratcheted for the batch-safety opt-in lines (#327);
  // write again for the Phase 5 test offer (test-run.md).
  // pre-delete snapshot capture + config-snapshots reference (Wave 2).
  write: 1700,
  // Cards adoption pointer (#389).
  diagnose: 1500,
  // Report-shape declaration line (shared output shapes); repair dedup,
  // attention-threshold definition, cause↔symptom linking (2026-h2 Wave 1c).
  health: 1350,
  // post-publish device verification step (2026-h2 Wave 1a).
  // User-assisted capture readiness sequence (#394).
  mqtt: 1500,
  // pre-delete snapshot capture (Wave 2); apply-test offer with the
  // high-consequence carve-out (Wave 3).
  scene: 1600,
  // buffering settle-window on verify (2026-h2 Wave 1a).
  // Cards adoption pointer (#389).
  media: 1250,
  // test-offer single-confirmation + reference bullets (test-run.md);
  // ratcheted for the owning-skill deferral table + high-consequence
  // confirmation rule (Wave 0), and again for the differentiated verify
  // block (transitions, stateless targets, canonical area expansion,
  // scene-timestamp verify) + capability gate (2026-h2 Wave 1a); runtime
  // event/webhook and alarm/lock contracts (2026-h2 Wave 4).
  // User-assisted proof bullet (#394).
  "service-call": 2650,
  // Carries the canonical File-Change Preview example — the only layout
  // source for file edits; concrete examples are what make a card renderable.
  // Sibling-survival verification (Wave 1b) + yaml snapshot capture with
  // stored path (Wave 2) + TS-check application at write time (Wave 3).
  "yaml-config": 1450,
  // Confirmation-code terminology replacing "token" wording (#392).
  todo: 1250,
  // batch-safety opt-in with the merged-save card rule (#327);
  // safety-backup offer (Wave 0) + drift check before the full-document
  // save (Wave 1a) + pre-delete/pre-save snapshot capture (Wave 2).
  // Cards adoption pointer (#389).
  dashboard: 1450,
  // Cards adoption pointer (#389).
  updates: 1250,
  // batch-safety alignment: batch code format + cap-split rule (#327);
  // purge quantification, glob expansion, apply_filter semantics
  // (2026-h2 Wave 1b).
  maintenance: 1400,
  // blueprint payload examples; integration onboarding and runtime
  // events/webhooks moved to owning skills in Wave 4.
  fallback: 2300,
  // semantic-slot note on the read templates (Wave 0); pre-write cross-field
  // constraint checks + drift-check step (Wave 1); pre-delete snapshot
  // capture (Wave 2).
  helper: 3850,
  // Suggestion Block item-shape pointer (shared output shapes); scene/
  // dashboard first-class targets with flow adaptation (2026-h2 Wave 3).
  // Quick-fix Preview Card reference (#389).
  review: 4550,
};
const DEFAULT_WORD_BUDGET = 1150;

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
      // Agent Skills open-standard alignment (masterplan A6): license and a
      // compatibility hint are required; metadata/allowed-tools stay banned.
      expect(fm.license, `${name}: license must be MIT`).toBe("MIT");
      const compatibility = fm.compatibility ?? "";
      expect(compatibility, `${name}: compatibility hint required`).toContain("ha-nova CLI");
      expect(compatibility.length, `${name}: compatibility over spec limit`).toBeLessThanOrEqual(500);
    }
  });

  it("tells agents how to install the missing CLI in the onboarding skill", () => {
    const onboarding = readFileSync(subskillPath("onboarding"), "utf8");
    expect(onboarding).toContain("the CLI is not installed");
    // Stable Install Contract: released skills must never bootstrap from the
    // moving main branch — the guidance points at the tagged release.
    expect(onboarding).toContain("releases/latest");
    expect(onboarding).not.toContain("main/install.sh");
  });

  it("keeps skill files on the real relay/trace CLI syntax", () => {
    // Regression: the diagnose skill shipped a non-existent `trace --entity`
    // flag. The trace CLI takes the entity positionally
    // (cli/trace.go: `trace <latest|list|get> <entity_id> [run_id] [--json]`).
    for (const file of ALL_SKILL_MD_FILES) {
      const content = readFileSync(file, "utf8");
      expect(
        /ha-nova trace[^\n]*--entity\b/.test(content),
        `${file}: 'ha-nova trace' takes the entity positionally, there is no --entity flag`,
      ).toBe(false);
    }
  });

  it("keeps the context skill on the A6 frontmatter standard too", () => {
    const fm = parseFrontmatter(readFileSync("skills/ha-nova/SKILL.md", "utf8"));
    expect(fm.license, "ha-nova: license must be MIT").toBe("MIT");
    const compatibility = fm.compatibility ?? "";
    expect(compatibility, "ha-nova: compatibility hint required").toContain("ha-nova CLI");
    expect(compatibility.length, "ha-nova: compatibility over spec limit").toBeLessThanOrEqual(500);
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

  it("covers every sub-skill by exactly one safety class", () => {
    for (const name of SUBSKILLS) {
      expect(
        MUTATION_SKILLS.has(name) !== READ_ONLY_SKILLS.has(name),
        `${name}: must be in exactly one of MUTATION_SKILLS / READ_ONLY_SKILLS`,
      ).toBe(true);
    }
  });

  it("opens every mutation-capable Safety section with the canonical Safety Core", () => {
    expect(SAFETY_CORE_BLOCKS.mutation, "SSOT block missing in skill-architecture.md").toContain(
      "Preview before write",
    );
    for (const name of SUBSKILLS) {
      if (!MUTATION_SKILLS.has(name)) continue;
      const body = sectionBody(readFileSync(subskillPath(name), "utf8"), "Safety");
      expect(
        body.trimStart().startsWith(SAFETY_CORE_BLOCKS.mutation),
        `${name}: ## Safety must open with the byte-identical Safety Core (SSOT: skill-architecture.md)`,
      ).toBe(true);
    }
  });

  it("opens every read-only Safety section with the canonical read-only core", () => {
    expect(SAFETY_CORE_BLOCKS.readOnly, "SSOT block missing in skill-architecture.md").toContain(
      "Read-only skill",
    );
    for (const name of SUBSKILLS) {
      if (!READ_ONLY_SKILLS.has(name)) continue;
      const body = sectionBody(readFileSync(subskillPath(name), "utf8"), "Safety");
      expect(
        body.trimStart().startsWith(SAFETY_CORE_BLOCKS.readOnly),
        `${name}: ## Safety must open with the byte-identical read-only core (SSOT: skill-architecture.md)`,
      ).toBe(true);
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
      const match = content.match(/\b(?:[SRPMFH]|SC|HX|TS|D)-\d{2}\b/);
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
