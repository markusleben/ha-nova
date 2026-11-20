import { readdirSync, readFileSync, statSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";

import { describe, expect, it } from "vitest";

// Word-budget ratchets for the dynamically discovered skill tree.

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

// Word budgets per sub-skill: words in its SKILL.md plus the FOLDED_INTO_BUDGET
// files. Ratchets: lower one when a skill shrinks; a raise needs its reason here
// and in the PR body (AGENTS.md → Review), the history stays in git. Above the
// default, each entry names what its skill carries.
const WORD_BUDGETS: Record<string, number> = {
  backup: 1210, // typed-verb fast path in the Safety Core
  notify: 1590, // platform payloads, household routing, delivery honesty
  "entity-discovery": 2460, // state snapshots, alias fallback, device siblings
  energy: 1360, // preference drift checks and snapshot recovery
  admin: 1300, // consumer checks, snapshots, tag and person lifecycle
  organize: 1580, // sibling re-enable, bulk target resolution
  camera: 1290, // verification frames and restart pointers
  write: 5600, // folds one-shot automations and blueprints; test offer, batch safety
  hacs: 2330, // Marketplace lifecycle: schema guard, reconcile, migration backup
  device: 1990, // swap, onboard, retire across eight owning skills
  diagnose: 1850, // log facts, trigger predicate, script branch
  health: 2130, // report modes, repairs, child-device fields
  mqtt: 1670, // device verification, capture readiness, Z2M bridge topics
  scene: 1900, // snapshots, test offer, consumer checks, restore services
  media: 1630, // search before browse, queue, settle window
  "service-call": 8030, // folds domain fields, raw routes, indirect actuation
  "yaml-config": 1720, // the canonical File-Change Preview example
  todo: 1560, // grouped item operations with pre-apply revalidation
  dashboard: 1710, // merged saves, drift checks, card templates
  updates: 1750, // update-all guardrails and update-state drift gate
  maintenance: 1550, // batch cap split, purge quantification
  fallback: 4680, // folds relay-ready; custom integration configuration APIs
  helper: 4540, // cross-field checks, flows, group members, re-confirm
  read: 1210, // fail-closed truncation envelopes
  review: 5070, // quick-fix cards and deep-read rules
  calendar: 1330, // recurring series and their verification
  "integration-setup": 2050, // credential recovery, options and reconfigure flows
  assist: 1540, // pipeline create and custom sentences
  onboarding: 690,
  "external-sources": 990,
  history: 1060,
};
const DEFAULT_WORD_BUDGET = 1150;

// Sections carved out of a SKILL.md keep counting against that skill's word
// budget. Add a file here whenever a split moves prose out of a budgeted file
// — otherwise the ratchet resets itself every time a file gets long.
const FOLDED_INTO_BUDGET: Record<string, string[]> = {
  fallback: ["relay-ready.md"],
  // #530 split the domain field tables out of service-call/SKILL.md. Leaving
  // it unfolded did exactly what the comment above warns about: the ceiling
  // measured 4252 while the skill really costs 5109, and the next split would
  // have reset it again.
  "service-call": ["domain-fields.md", "raw-routes.md", "../ha-nova/indirect-actuation.md"],
  // Blueprints split out of write/SKILL.md (the write route diet).
  write: ["../ha-nova/one-shot-automations.md", "blueprints.md"],
};

// Secondary Markdown that is a standalone reference, not prose split out of
// one owning SKILL.md. The classification is exhaustive so a new unregistered
// file cannot silently reset a word budget.
const STANDALONE_MARKDOWN = new Set([
  "skills/energy/energy-reference.md",
  "skills/entity-discovery/computed-answers.md",
  "skills/esphome/esphome-checks.md",
  "skills/ha-nova/automation-patterns.md",
  "skills/ha-nova/batch-safety.md",
  "skills/ha-nova/best-practices.md",
  "skills/ha-nova/bulk-patterns.md",
  "skills/ha-nova/capability-answer.md",
  "skills/ha-nova/config-snapshots.md",
  "skills/ha-nova/confirmed-actions.md",
  "skills/ha-nova/consumer-discovery-preflight.md",
  "skills/ha-nova/grouped-change-set.md",
  "skills/ha-nova/helper-flow-schemas.md",
  "skills/ha-nova/helper-schemas.md",
  "skills/ha-nova/index.md",
  "skills/ha-nova/input-capability-preflight.md",
  "skills/ha-nova/live-schema-preflight.md",
  "skills/ha-nova/membership-resolution.md",
  "skills/ha-nova/mobile-notification-composition.md",
  "skills/ha-nova/outcome-verification.md",
  "skills/ha-nova/output-cards.md",
  "skills/ha-nova/output-rules.md",
  "skills/ha-nova/payload-schemas.md",
  "skills/ha-nova/recovery-coverage.md",
  "skills/ha-nova/recovery-workflows.md",
  "skills/ha-nova/relay-api.md",
  "skills/ha-nova/safe-refactoring.md",
  "skills/ha-nova/session-bootstrap.md",
  "skills/ha-nova/smallest-solution.md",
  "skills/ha-nova/starter-proposals.md",
  "skills/ha-nova/template-guidelines.md",
  "skills/ha-nova/test-run.md",
  "skills/ha-nova/threshold-calibration.md",
  "skills/ha-nova/update-revert.md",
  "skills/ha-nova/write-safety.md",
  "skills/hacs/hacs-commands.md",
  "skills/health/availability-analysis.md",
  "skills/health/checkup.md",
  "skills/maintenance/maintenance-reference.md",
  "skills/migrate/source-mappings.md",
  "skills/organize/display-fields.md",
  "skills/review/checks.md",
  "skills/review/checks-extended.md",
  "skills/updates/advisor.md",
  "skills/updates/apps.md",
  "skills/yaml-config/www-media.md",
].map((file) => resolve(file)));

function subskillPath(name: string): string {
  return join(SKILLS_ROOT, name, "SKILL.md");
}

describe("skill word-budget contract", () => {
  it("keeps split contracts folded into their owning skill budgets", () => {
    expect(FOLDED_INTO_BUDGET).toEqual({
      fallback: ["relay-ready.md"],
      "service-call": ["domain-fields.md", "raw-routes.md", "../ha-nova/indirect-actuation.md"],
      write: ["../ha-nova/one-shot-automations.md", "blueprints.md"],
    });
    const folded = Object.entries(FOLDED_INTO_BUDGET).flatMap(([name, files]) => {
      const dir = dirname(subskillPath(name));
      return files.map((file) => resolve(dir, file));
    });
    const secondaryMarkdown = ALL_SKILL_MD_FILES
      .filter((file) => basename(file) !== "SKILL.md")
      .map((file) => resolve(file))
      .sort();
    expect([...folded, ...STANDALONE_MARKDOWN].sort()).toEqual(secondaryMarkdown);
  });

  it("keeps every sub-skill within its word budget", () => {
    for (const name of SUBSKILLS) {
      // Files split OUT of a budgeted SKILL.md count against the same budget.
      // Splitting for the ~400-line guardrail is fine; using it to escape the
      // word ratchet is not, and the split is invisible here otherwise.
      const dir = resolve(subskillPath(name), "..");
      const counted = [
        subskillPath(name),
        ...(FOLDED_INTO_BUDGET[name] ?? []).map((file) => join(dir, file)),
      ];
      const wordCount = counted
        .map((file) => readFileSync(file, "utf8").trim().split(/\s+/).length)
        .reduce((total, count) => total + count, 0);
      const limit = WORD_BUDGETS[name] ?? DEFAULT_WORD_BUDGET;
      expect(
        wordCount,
        `skills/${name}/ has ${wordCount} words including its split-out files (limit ${limit})`,
      ).toBeLessThan(limit);
    }
  });
});
