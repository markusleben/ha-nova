import { readdirSync, readFileSync, statSync } from "node:fs";
import { basename, dirname, join, resolve } from "node:path";

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
  fallback: "Bootstrap (before Home Assistant tasks)",
};
const DEFAULT_BOOTSTRAP = "Bootstrap (once per session)";
const SESSION_BOOTSTRAP_REF = "../ha-nova/session-bootstrap.md";

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
  "hacs",
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
  // Platform-specific payloads plus presence-based household routing in the
  // combined audit train (measured 1382 before the bounded-wait removal).
  notify: 1430,
  // State-snapshot queries ("is everything closed?") and the alias fallback
  // that finally reaches the names a household actually says (#527, 1318).
  // Codex round 3: a motorized window or garage door is a cover, so an
  // open-state snapshot that reads only binary_sensor answers wrong
  // (measured 1374).
  // Presence-conditional recipient resolution; Codex round 1 required real
  // /api/services discovery instead of deriving a notify service name from
  // tracker ids, which prove no association (#527, measured 1264).
  // Ceiling covers the COMBINED merge: #516 adds the honest bounded window
  // and #527 the household routing, each measured alone. Trial-merging the
  // train lands at 1382.
  // Audit train: the ceiling is the MAX of both branches — each
  // measured only its own tree.
  "entity-discovery": 2100,
  // pre-save snapshot capture + snapshot recovery guidance (Wave 2).
  // Pre-write drift check before save_prefs (#514, measured 1257): the
  // post-save deep-equal check reported a lost foreign edit instead of
  // preventing it.
  // Codex round 2: first-time setup has no prefs document, so the drift
  // reread needs absence as an explicit basis (measured 1283).
  energy: 1300,
  // consumer checks before area delete/rename/disable (Wave 1b) + metadata
  // snapshot capture (Wave 2).
  // Grouped-change-set opt-in + flow wiring (#391).
  // #513/#530 added routing rows and the tag/person lifecycle notes: the
  // default 1150 left TWO words of headroom, which the next sentence breaks.
  admin: 1300,
  organize: 1300,
  // write/mqtt ratcheted for the batch-safety opt-in lines (#327);
  // write again for the Phase 5 test offer (test-run.md).
  // pre-delete snapshot capture + config-snapshots reference (Wave 2).
  // input-capability gate (#396) + consumer-discovery routing (#397).
  // Fail-closed consumer scan: canonical filter with inline recreate
  // fallback at both search/related call sites + delete-direction
  // clarification (#489).
  // Self-describing update-revert checkpoint receipts (#483).
  // Threshold-calibration preflight step (#484), extended to
  // compared-signal swaps (Codex round 5) and create-time stored threshold
  // setters (Codex round 6). Merge-train combination with the #483 receipt
  // lines and the #452 draft rule (measured 2182).
  // One-shot intent routing to the self-disabling pattern (#527, 2221).
  // Includes one-shot-automations.md after the audit exposed that the split
  // had reset this ratchet (measured 5356 on #543).
  write: 5380,
  // HACS lifecycle: schema guard, reconcile loops, consumer discovery,
  // migration backup gate, category-appropriate verification (#478);
  // review rounds added pin-durability branches, the uninstall apply
  // step, and the prerelease-toggle rule (measured 2217).
  hacs: 2250,
  // Cards adoption pointer (#389).
  // Charset restriction on the value interpolated into the log-filter regex,
  // plus the escaping rule Codex round 1 added: an entity_id is not literal
  // either, since its domain separator is a regex wildcard (#518, 1536).
  // Codex round 7: the log filter OR-ed a severity into the identifier, so
  // every unrelated error line read as evidence (measured 1611).
  diagnose: 1660,
  // Report-shape declaration line (shared output shapes); repair dedup,
  // attention-threshold definition, cause↔symptom linking (2026-h2 Wave 1c).
  // Table-first redesign: report modes, block shape, ten-block order,
  // behavior rules, private source fields, canonical detector/system
  // blocks retained (#440).
  health: 2100,
  // post-publish device verification step (2026-h2 Wave 1a).
  // User-assisted capture readiness sequence (#394).
  mqtt: 1500,
  // pre-delete snapshot capture (Wave 2); apply-test offer with the
  // high-consequence carve-out (Wave 3).
  // Fail-closed consumer check with canonical-filter recreate pointer (#489).
  // #452 draft rule pushed the measured count to exactly 1650.
  // Capture attributes named per domain, with the state-attribute vs
  // service-parameter split that decides whether a partial cover survives
  // (#530 Codex round 1, measured 1703).
  // Codex rounds 3-4: a range thermostat needs both setpoints captured or the
  // scene restores the mode without either boundary, and a fan restores
  // oscillation and direction too (measured 1748).
  // Codex round 5: climate reproduction restores preset/fan/swing/humidity
  // through separate services; each omission is a silent non-restore
  // (measured 1787).
  scene: 1840,
  // buffering settle-window on verify (2026-h2 Wave 1a).
  // Cards adoption pointer (#389).
  // Search-before-browse (player and media_source), queue placement, and the
  // shuffle/repeat services whose bits the gate table already carried but no
  // flow step used (#530, measured 1399).
  // Codex round 4: search_media takes a required entity_id, so the pinned
  // call shape had to become a full payload (measured 1410).
  // Codex round 6: queueing and mode changes have no signal in the fields the
  // generic verify step names — one has none at all, the other has its own
  // (measured 1544).
  media: 1600,
  // test-offer single-confirmation + reference bullets (test-run.md);
  // ratcheted for the owning-skill deferral table + high-consequence
  // confirmation rule (Wave 0), and again for the differentiated verify
  // block (transitions, stateless targets, canonical area expansion,
  // scene-timestamp verify) + capability gate (2026-h2 Wave 1a); runtime
  // event/webhook and alarm/lock contracts (2026-h2 Wave 4).
  // User-assisted proof bullet (#394).
  // Grouped-change-set opt-in + grouped-menu exception (#391).
  // Threshold-calibration hook incl. scene.apply coverage (#484 R10).
  // #452 draft rule on top of the branch ratchet (measured 2780).
  // Tier hardening (#513): owning-skill deferral rows for recorder/calendar/
  // todo/backup/camera-power/conversation with the read-only-response
  // carve-out, the Supervisor-lifecycle block with its self-amputation
  // refusal, the indirect-actuation gate pointer, and the
  // tier-follows-the-performed-action rule with the disruptive split. The
  // gate mechanics themselves live in skills/ha-nova/indirect-actuation.md
  // so every caller shares one contract. Codex round 2 added the App-restart
  // disruption note; round 4 narrowed the hassio row from a wildcard to the
  // named lifecycle services, because restores and App updates have owning
  // skills that refuse or gate them (measured 3170).
  // Codex round 7: the DIRECT fire-an-event path needed the same
  // unenumerable-listener escalation as the stored event: action path
  // (measured 3204).
  // Codex round 9: the Flow pointer listed fewer trigger-source domains than
  // the gate it points at, so a counter or timer write never entered it
  // (measured 3252).
  // Codex round 10: a Template button runs a stored action, so `button.press`
  // is an indirect run rather than a toggle (measured ~3270).
  // Domain-depth pointer + the two cross-domain value/feature-bit rules
  // (#530, measured 2849). The headroom above that anticipates the #513
  // indirect-actuation lines landing in the same train (measured 3089
  // there); whichever merges second must stay under this ceiling or bump it
  // again with its own measurement.
  // Merged in the audit train: the ceiling is the MAX of both
  // branches, not either one — each measured only its own tree.
  // Description rewritten in the user's own control vocabulary (#518,
  // measured 2811). #513 and #530 raise this further in the same train.
  // Audit train: the ceiling is the MAX of both branches — each
  // measured only its own tree.
  // Relative-step parameters and the post-batch keep-it-as-a-scene offer
  // (#527, measured 2888). #513/#518/#530 raise this further in the same train.
  // Ceiling covers the COMBINED merge: #513, #530, #518 and #527 all add to
  // service-call and each measured itself in isolation. A prefix scan of the
  // train shows only the LAST merge overflowing; at 3606 after round 6.
  // Combined-merge ceiling with headroom, deliberately. Four PRs edit
  // service-call and each measured itself alone; chasing the exact merged
  // total each round just reopens this file. The per-PR ceilings still hold
  // the ratchet — this one only has to keep main from breaking on the last
  // merge. Measured 3963 at the time of writing.
  // Audit train: the ceiling is the MAX of both branches — each
  // measured only its own tree.
  // Includes domain-fields.md and indirect-actuation.md; both are contracts
  // split out of this skill (measured 8777 on #543).
  "service-call": 8800,
  // Carries the canonical File-Change Preview example — the only layout
  // source for file edits; concrete examples are what make a card renderable.
  // Sibling-survival verification (Wave 1b) + yaml snapshot capture with
  // stored path (Wave 2) + TS-check application at write time (Wave 3).
  // Pre-write drift check before the whole-file write (#514): the single-step
  // .bak reported success while reverting a concurrent edit. Codex round 1
  // moved the check behind the snapshot capture and split the brand-new-file
  // case, whose basis is absence rather than content. Round 2 replaced the
  // list_dir absence probe with an exact-path one: list_dir caps at 500
  // entries and can report an existing file as absent (measured 1588).
  "yaml-config": 1610,
  // Confirmation-code terminology replacing "token" wording (#392).
  // #452 canonical smallest-solution draft rule (17 words).
  // Grouped item operations: four shopping-list adds should not cost four
  // confirmation rounds (#527, measured 1315).
  // Codex round 2: one confirmation, but still one read-back per operation —
  // the grouped ledger is fail-fast and a trailing read would record a
  // silently ignored write as applied (measured 1357).
  // Codex round 11: the grouped batch needed the contract's pre-apply
  // revalidation; post-write read-backs cannot see a concurrent edit
  // (measured 1420).
  todo: 1560,
  // batch-safety opt-in with the merged-save card rule (#327);
  // safety-backup offer (Wave 0) + drift check before the full-document
  // save (Wave 1a) + pre-delete/pre-save snapshot capture (Wave 2).
  // Cards adoption pointer (#389).
  // lovelace/config/save payload shape — the `config` key that carries the
  // whole document was named nowhere (#518, measured 1454).
  // Codex round 1: the default dashboard is selected by OMITTING url_path;
  // an explicit null is rejected (measured 1471).
  dashboard: 1490,
  // Cards adoption pointer (#389); pre-write update-state drift gate.
  // #452 canonical smallest-solution draft rule (17 words).
  // HA 2026.7 "Update all" semantics: guardrails mirrored, call shape
  // deliberately not; batches never override selected_tag pins (#478
  // follow-up, Codex P1).
  // #452 canonical draft rule on top of the update-all semantics, plus the
  // explicit-version handoff to ha-nova:hacs (measured 1569).
  updates: 1590,
  // batch-safety alignment: batch code format + cap-split rule (#327);
  // purge quantification, glob expansion, apply_filter semantics
  // (2026-h2 Wave 1b).
  // Reverse boundary against health, so the long-unavailable report reads as
  // a cleanup qualifier rather than a status answer (#518, measured 1408).
  // Codex round 6: the long-unavailable boundary has to be advertised on the
  // receiving side too, or routing never reaches it (measured 1427).
  maintenance: 1470,
  // blueprint payload examples; integration onboarding and runtime
  // events/webhooks moved to owning skills in Wave 4.
  // Custom-integration configuration APIs section + write-probing
  // asymmetry guardrails (#493), incl. the parameterless-WS-write
  // restriction (Codex P1).
  // Flow step 3c now names the endpoint-type table it depends on instead of
  // leaving the file's load-bearing content unreferenced; Codex round 1
  // scoped that requirement to endpoints the table actually covers and named
  // the researched-schema path for the rest (#518, measured 2799).
  // Codex round 8: a config-entry flow answers from its own live response,
  // never from the research schema (measured 2863).
  // Capability-map completion (#516): the map is fallback's routing index, so
  // a missing row means Flow step 1 finds nothing and the agent improvises.
  // Added integration entry lifecycle (with its own Relay-Ready mechanics —
  // remove deletes every device the entry owns), Matter/Thread, custom
  // sentences, local-calendar creation, and device categories; corrected the
  // Supervisor premise and the bounded-event roadmap (measured 3083).
  // Codex round 1 (#516): the Flow branches on the map's STATUS column, so
  // "Relay-Ready" claims in prose were unreachable — bounded event capture,
  // Thread status reads and custom sentences now carry their own rows AND
  // their own mechanics sections (measured 3361).
  // Codex round 2: a malformed sentence file takes every custom sentence
  // with it, so detecting that in the assist test is not enough — the flow
  // needs the restore-and-retest path (measured 3439).
  // Codex round 3: conversation.reload does not load a new intent_script, so
  // testing before that lands makes a valid sentence file look broken and
  // triggers the rollback (measured 3490).
  // Codex round 3: configuration.yaml validate-first plus the honest
  // intent_script restart note — an invalid file blocks the next boot
  // (measured 3584).
  // Ceiling covers the COMBINED merge: three PRs add to fallback (#525 stale
  // refs, #518 routing, #516 reference truth) and each measured itself in
  // isolation. Trial-merging the train lands at 3646.
  // Ceiling covers the COMBINED merge of the three fallback-touching PRs
  // (#525, #518, this one), each measured in isolation. The train lands at
  // 3710 after #518's config-entry schema clause.
  // Covers SKILL.md PLUS relay-ready.md (the fold above) for the COMBINED
  // merge of the three fallback-touching PRs, with headroom so a late round
  // does not reopen this file. Measured 4413.
  // Audit train: the ceiling is the MAX of both branches — each
  // measured only its own tree.
  // Includes relay-ready.md; measured 4938 on #543.
  fallback: 5000,
  // semantic-slot note on the read templates (Wave 0); pre-write cross-field
  // constraint checks + drift-check step (Wave 1); pre-delete snapshot
  // capture (Wave 2).
  // Grouped-change-set opt-in + final-block clarifier (#391).
  // Smallest-complete-solution routing for feature offers (#452).
  // Threshold-family calibration preflight wiring (#484); merge-train
  // combination measured 3970.
  helper: 3990,
  // Suggestion Block item-shape pointer (shared output shapes); scene/
  // dashboard first-class targets with flow adaptation (2026-h2 Wave 3).
  // Quick-fix Preview Card reference (#389).
  // Quick-Fix physical-access exclusion (#513): access-granting corrections
  // hand off to service-call instead of running at the natural tier
  // (measured 4607).
  // Codex round 7 (#513): the Quick-Fix exclusion caught direct access calls
  // but not trigger-source corrections — resetting a helper is exactly what
  // another automation answers by unlocking a door (measured 4654).
  // Codex round 12: the Quick-Fix exclusion keyed on entering the gate rather
  // than on its verdict, so a clean scan still blocked the fix (measured 4698).
  review: 4820,
  // Codex round 2 (#518): the entrypoint carried its own copy of the
  // verify-before-flag gate, which contradicted the corrected one in
  // checks.md and could suppress accepted-but-dangerous findings; trace
  // ANALYSIS moved to diagnose, leaving review with trace evidence only
  // (measured 4650).
  // Ceiling covers the COMBINED merge: #525, #513 and #518 all add to review
  // and each measured itself in isolation. Trial-merging the train lands at
  // 4758.
  // Audit train: the ceiling is the MAX of both branches — each
  // measured only its own tree.
  // #452 wires the canonical 17-word smallest-solution draft rule into every
  // write-flow skill; calendar and integration-setup sat within 17 words of
  // the default cap (review/todo/updates ratchets applied on their entries).
  calendar: 1175,
  "integration-setup": 1175,
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
  "service-call": ["domain-fields.md", "../ha-nova/indirect-actuation.md"],
  write: ["../ha-nova/one-shot-automations.md"],
};

// Secondary Markdown that is a standalone reference, not prose split out of
// one owning SKILL.md. The classification is exhaustive so a new unregistered
// file cannot silently reset a word budget.
const STANDALONE_MARKDOWN = new Set([
  "skills/energy/energy-reference.md",
  "skills/ha-nova/agents/apply-agent.md",
  "skills/ha-nova/agents/resolve-agent.md",
  "skills/ha-nova/automation-patterns.md",
  "skills/ha-nova/batch-safety.md",
  "skills/ha-nova/best-practices.md",
  "skills/ha-nova/bulk-patterns.md",
  "skills/ha-nova/config-snapshots.md",
  "skills/ha-nova/consumer-discovery-preflight.md",
  "skills/ha-nova/grouped-change-set.md",
  "skills/ha-nova/helper-flow-schemas.md",
  "skills/ha-nova/helper-schemas.md",
  "skills/ha-nova/input-capability-preflight.md",
  "skills/ha-nova/output-rules.md",
  "skills/ha-nova/payload-schemas.md",
  "skills/ha-nova/relay-api.md",
  "skills/ha-nova/safe-refactoring.md",
  "skills/ha-nova/session-bootstrap.md",
  "skills/ha-nova/smallest-solution.md",
  "skills/ha-nova/template-guidelines.md",
  "skills/ha-nova/test-run.md",
  "skills/ha-nova/threshold-calibration.md",
  "skills/ha-nova/update-revert.md",
  "skills/ha-nova/write-safety.md",
  "skills/hacs/hacs-commands.md",
  "skills/health/availability-analysis.md",
  "skills/maintenance/maintenance-reference.md",
  "skills/review/checks.md",
].map((file) => resolve(file)));

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

  it("routes every independently loadable sub-skill through the shared session bootstrap", () => {
    for (const name of SUBSKILLS) {
      const content = readFileSync(subskillPath(name), "utf8");
      const bootstrapHeading = BOOTSTRAP_HEADINGS[name] ?? DEFAULT_BOOTSTRAP;
      const bootstrap = sectionBody(content, bootstrapHeading);
      expect(
        bootstrap,
        `${name}: direct loading must retain HA NOVA + Relay update discovery`,
      ).toContain(SESSION_BOOTSTRAP_REF);
      expect(
        statSync(resolve(dirname(subskillPath(name)), SESSION_BOOTSTRAP_REF)).isFile(),
        `${name}: shared session bootstrap reference must resolve from the skill directory`,
      ).toBe(true);
      const sharedContract = bootstrap.indexOf(SESSION_BOOTSTRAP_REF);
      const firstRelayCommand = bootstrap.indexOf("ha-nova relay");
      if (firstRelayCommand >= 0) {
        expect(
          sharedContract,
          `${name}: shared session bootstrap must run before the first relay command`,
        ).toBeLessThan(firstRelayCommand);
      }
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

  it("keeps split contracts folded into their owning skill budgets", () => {
    expect(FOLDED_INTO_BUDGET).toEqual({
      fallback: ["relay-ready.md"],
      "service-call": ["domain-fields.md", "../ha-nova/indirect-actuation.md"],
      write: ["../ha-nova/one-shot-automations.md"],
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
      const counted = [subskillPath(name), ...(FOLDED_INTO_BUDGET[name] ?? []).map((f) => join(dir, f))];
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
