// Canonical wording guard: claims that were wrong or said two ways must not
// come back silently. The banned phrases (with why + what to say instead) are
// in banned-wording.json, shared with hanova.app's scripts/check-sync.sh.
//
// Canonical wording (one way, everywhere):
// - ChatGPT: works on the web, on a computer; needs a paid plan that lets you
//   add your own plugins; we tested Pro; the free plan can't; on a phone it
//   can only answer. ChatGPT says "plugin", Claude says "connector".
// - Claude: on the web, on a computer, and in its phone app. Its connectors
//   are under Customize → Connectors. Team/Enterprise: the owner of your
//   Claude plan adds it once for everyone ("Add for my organization").
// - Home Assistant Link (formerly Home Assistant Cloud; Nabu Casa): the old
//   name appears once, as that explanation. Technical ids (`cloud`) stay.
// - "App", not "Add-on". The App is the NOVA App, under Settings → Apps →
//   NOVA Relay. The NOVA page is NOVA in the Home Assistant sidebar.
// - Confirmations: a window on the computer or a tap on the linked phone;
//   with neither, a four-digit code. Pairing uses a six-digit code that works
//   once, for 10 minutes. Phone buttons: press and hold on iPhone, pull down
//   on Android.
// - Only the Home Assistant owner can turn on and use chat apps.
// - Setup and NOVA page prose: "AI app", "password store", no MCP/OAuth
//   except a quoted on-screen label.
// Source: the 2026-10-23 plain-language audit (internal).
import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

type Rule = {
  id: string;
  scope: "all" | "pages";
  pattern?: string;
  flags?: string;
  quotedOk?: boolean;
  sha256?: string[];
  why: string;
  use: string;
};

const { rules } = JSON.parse(readFileSync("tests/docs/banned-wording.json", "utf8")) as { rules: Rule[] };

// True exceptions: the hit's line must contain `line`. Stale entries fail.
// line "" = the whole file.
const allow: { rule: string; file: string; line: string; reason: string }[] = [
  // README changes only in a release-prep PR (AGENTS.md).
  { rule: "one-time-code", file: "README.md", line: "You pair with a one-time code", reason: "fix at next release-prep: six-digit code" },
  { rule: "one-time-code", file: "README.md", line: '| *"Unlock the front door"* |', reason: "fix at next release-prep: four-digit code" },
  // The contributor index mirrors the skills' own term, "the one-time code" (pinned in tests/skills).
  { rule: "one-time-code", file: "docs/reference/skill-architecture.md", line: "", reason: "mirrors the skill prose term" },
  { rule: "add-on", file: "docs/reference/skill-architecture.md", line: "**Terminology rule:**", reason: "states the App rule itself" },
  // The menu name people see on Home Assistant before 2026.2.
  { rule: "add-on", file: "cli/setupweb/strings.en.json", line: "(older Home Assistant: Settings > Add-ons)", reason: "old on-screen menu name" },
  { rule: "add-on", file: "docs/reference/update-guide.md", line: "Apps are still called Add-ons", reason: "old on-screen menu name" },
  { rule: "add-on", file: "nova/DOCS.md", line: "renamed Add-ons to Apps", reason: "explains the rename" },
  { rule: "add-on", file: "docs/reference/ha-doc-gate-2026-02-26.md", line: "Apps (formerly add-ons)", reason: "dated record; explains the rename" },
  { rule: "add-on", file: "docs/reference/ha-doc-gate-2026-02-26.md", line: "### 2) Apps / Add-ons", reason: "dated record; explains the rename" },
  { rule: "add-on", file: "docs/reference/hosted-demo.md", line: 'the "Demo add-on" device', reason: "Home Assistant's own demo device name, quoted" },
];

const pages = ["cli/setupweb/strings.en.json", "nova/src/http/handlers/nova-page-view.ts"];
const docs = [
  "README.md",
  ...readdirSync("docs/reference").filter((f) => f.endsWith(".md")).map((f) => `docs/reference/${f}`),
  "nova/DOCS.md",
  "nova/README.md",
  "nova/translations/en.yaml",
  ...pages,
];

// Go: string literals the CLI shows a person — the say/phone text files, and
// printHuman* lines elsewhere. ponytail: per line, so a printHuman call whose
// string starts on the next line is missed; parse the AST if that bites.
const sayFiles = /^(say_lines|confirm_say|confirm_say_text|confirm_phone_text)\.go$/;
function goLines(file: string): string[] {
  const name = file.slice(4);
  return readFileSync(file, "utf8")
    .split("\n")
    .map((line) =>
      !line.trim().startsWith("//") && (sayFiles.test(name) || /\bprintHuman\w*\(/.test(line))
        ? (line.match(/"(?:[^"\\]|\\.)*"/g) ?? []).join(" ")
        : "",
    );
}
const goFiles = readdirSync("cli")
  .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
  .map((f) => `cli/${f}`);

const sha = (word: string) => createHash("sha256").update(word).digest("hex");

function hits(rule: Rule, line: string): string[] {
  if (rule.sha256) {
    return (line.toLowerCase().match(/[a-z0-9]+(?:-[a-z0-9]+)*/g) ?? []).filter((w) => rule.sha256!.includes(sha(w)));
  }
  const text = rule.quotedOk ? line.replace(/“[^”]*”/g, "“”") : line;
  return [...text.matchAll(new RegExp(rule.pattern!, `g${rule.flags ?? ""}`))].map((m) => m[0]);
}

const surfaces = [
  ...docs.map((file) => ({ file, lines: readFileSync(file, "utf8").split("\n") })),
  ...goFiles.map((file) => ({ file, lines: goLines(file) })),
];
const used = new Set<number>();
const found = new Map(
  rules.map((rule) => {
    const out: string[] = [];
    for (const { file, lines } of surfaces) {
      if (rule.scope === "pages" && !pages.includes(file)) continue;
      lines.forEach((line, i) => {
        for (const hit of hits(rule, line)) {
          const ok = allow.findIndex((a) => a.rule === rule.id && a.file === file && line.includes(a.line));
          if (ok >= 0) used.add(ok);
          else out.push(`${file}:${i + 1}: "${hit}" — ${rule.why} Use: ${rule.use}`);
        }
      });
    }
    return [rule.id, out];
  }),
);

describe("canonical wording", () => {
  it.each(rules.map((rule) => rule.id))("%s stays out of user-facing text", (id) => {
    expect(found.get(id)).toEqual([]);
  });

  it("has no stale allow-list entry", () => {
    expect(allow.filter((_, i) => !used.has(i))).toEqual([]);
  });
});
