import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

// Issue #392: prose must never call the typed code a token. Since the
// four-digit codes its user-facing name is the "one-time code from HA NOVA"
// (skills/ha-nova/output-rules.md → Localization). Auth terminology ("access token", relay token, LLAT) stays.

const SKILLS_ROOT = "skills";

const REFERENCE_DOCS = [
  "docs/reference/skill-architecture.md",
  "docs/reference/safety.md",
  "docs/reference/comparison.md",
];

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

// Confirmation-flavored compounds: banned everywhere, no exceptions.
const BANNED_COMPOUNDS = [
  /\btyped[- ]tokens?\b/i,
  /\bconfirmation tokens?\b/i,
  /\btokens? prompts?\b/i,
  /\btokens? confirmation\b/i,
  /\btokenized\b/i,
  /\btoken[- ]gated\b/i,
  /\bexact tokens?\b/i,
  /\btokens? (?:string|enforcement|tier|gate)s?\b/i,
  /\b(?:destructive|single-target|sub)[- ]tokens?\b/i,
  /\bshort tokens?\b/i,
  /\bplus tokens?\b/i,
  /\bdelete tokenization\b/i,
];

// Files that may use bare "token(s)" in an authentication/credential sense.
const AUTH_TOKEN_FILES = new Set([
  "skills/external-sources/SKILL.md",
  "skills/integration-setup/SKILL.md",
  "skills/read/SKILL.md",
  "skills/admin/SKILL.md",
  "skills/energy/SKILL.md",
  "skills/health/SKILL.md",
  "skills/onboarding/SKILL.md",
  "skills/review/checks.md",
  "skills/ha-nova/SKILL.md",
  "skills/ha-nova/output-rules.md",
  "skills/ha-nova/write-safety.md",
  "skills/ha-nova/recovery-coverage.md", // split out of write-safety.md (PR6), same auth-token rows
  "skills/ha-nova/batch-safety.md",
  "skills/ha-nova/relay-api.md",
  "docs/reference/safety.md",
  "docs/reference/comparison.md",
]);

// A bare "token" line in an allowlisted file must carry auth context — this is
// the checkable form of the issue's auth-language exception.
const AUTH_CONTEXT =
  /auth|LLAT|credential|secret|password|paste|placeholder|access[- ]token|API|long-lived|InfluxDB|read permission|URLs|relay|owner-level|OAuth|Bearer|SUPERVISOR|HA token|pairing|privacy|token cost/i;

const strip = (md: string): string =>
  md
    .replace(/```[\s\S]*?```/g, "") // fenced blocks (SSOT quotes, examples)
    .replace(/`[^`\n]*`/g, "") // inline code spans
    .replace(/"token"/g, ""); // the meta-rule idiom (never call it a "token")

interface Violation {
  line: number;
  text: string;
  reason: string;
}

function findTokenViolations(content: string, authAllowed: boolean): Violation[] {
  const violations: Violation[] = [];
  const stripped = strip(content);
  stripped.split("\n").forEach((line, idx) => {
    for (const pattern of BANNED_COMPOUNDS) {
      if (pattern.test(line)) {
        violations.push({ line: idx + 1, text: line.trim(), reason: `banned compound ${pattern}` });
        return;
      }
    }
    if (!/\btokens?\b/i.test(line)) return;
    if (!authAllowed) {
      violations.push({ line: idx + 1, text: line.trim(), reason: "bare token outside auth allowlist" });
    } else if (!AUTH_CONTEXT.test(line)) {
      violations.push({ line: idx + 1, text: line.trim(), reason: "bare token without auth context" });
    }
  });
  return violations;
}

describe("confirmation-code terminology (issue #392)", () => {
  it("keeps skill and reference prose free of confirmation-token jargon", () => {
    for (const file of [...ALL_SKILL_MD_FILES, ...REFERENCE_DOCS]) {
      const normalized = file.split("\\").join("/");
      const violations = findTokenViolations(readFileSync(file, "utf8"), AUTH_TOKEN_FILES.has(normalized));
      expect(
        violations,
        `${file} has token-wording violations:\n${violations.map((v) => `  L${v.line} (${v.reason}): ${v.text}`).join("\n")}`,
      ).toEqual([]);
    }
  });

  it("keeps the auth-language exception alive (real auth wording still passes)", () => {
    const externalSources = readFileSync("skills/external-sources/SKILL.md", "utf8");
    const admin = readFileSync("skills/admin/SKILL.md", "utf8");
    expect(externalSources).toContain("Never ask the user to paste a token into the conversation.");
    expect(admin).toContain("long-lived tokens");
  });

  it("pins the canonical localization rule the checker enforces", () => {
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8") + readFileSync("skills/ha-nova/output-cards.md", "utf8");
    expect(outputRules).toContain('the "one-time code from HA NOVA" (localized');
    expect(outputRules).toContain('Never call it a bare "code", "PIN", or "token" in user-facing output');
  });

  // Generic localization fixtures: destructive-card copy in English and a
  // localized (German) rendering both pass; token jargon fails; auth passes.
  it("validates destructive-card fixtures in English and localized form", () => {
    const englishCard = [
      "🗑️ Delete automation: Morning Lights",
      "⚠️ Nothing deleted yet.",
      "Type 4821 to delete — a plain yes can't confirm this.",
      "Type the one-time code from HA NOVA exactly as shown.",
    ].join("\n");
    const germanCard = [
      "🗑️ Automation löschen: Morgenlicht",
      "⚠️ Noch nichts gelöscht.",
      "Tippe 4821 zum Löschen — ein einfaches Ja reicht hier nicht.",
      "Gib den Einmalcode von HA NOVA exakt so ein.",
    ].join("\n");
    const tokenJargonCard = "Reply with the typed token 4821 to proceed.";
    const authSentence = "Never ask the user to paste an access token or password in chat.";

    expect(findTokenViolations(englishCard, false)).toEqual([]);
    expect(findTokenViolations(germanCard, false)).toEqual([]);
    expect(findTokenViolations(tokenJargonCard, false)).not.toEqual([]);
    expect(findTokenViolations(authSentence, true)).toEqual([]);
  });
});
