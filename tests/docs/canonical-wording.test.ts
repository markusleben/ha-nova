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
  words?: string;
  why: string;
  use: string;
};

const { rules } = JSON.parse(readFileSync("tests/docs/banned-wording.json", "utf8")) as { rules: Rule[] };

// True exceptions: `file` (or a folder ending in /) plus text the hit's line must contain. Stale entries fail.
const allow: { rule: string; file: string; line: string; reason: string }[] = [
  // The contributor index mirrors the skills' own term, "the one-time code" (pinned in tests/skills).
  ...[
    "impact preview + one-time code",
    "delete uses the one-time code",
    "else the one-time code), refuses owned services",
    "command/`set` topics take the one-time code",
    "everything else needs the one-time code",
    "list deletion keeps the one-time code",
    "- delete requires one-time code",
  ].map((line) => ({ rule: "one-time-code", file: "docs/reference/skill-architecture.md", line, reason: "mirrors the skill prose term" })),
  { rule: "add-on", file: "docs/reference/skill-architecture.md", line: "**Terminology rule:**", reason: "states the App rule itself" },
  // The menu name people see on Home Assistant before 2026.2.
  { rule: "add-on", file: "cli/setupweb/strings.en.json", line: "(older Home Assistant: Settings > Add-ons)", reason: "old on-screen menu name" },
  { rule: "add-on", file: "cli/testdata/setup-golden/", line: "(older Home Assistant: Settings > Add-ons)", reason: "old on-screen menu name" },
  { rule: "add-on", file: "docs/reference/update-guide.md", line: "Apps are still called Add-ons", reason: "old on-screen menu name" },
  { rule: "add-on", file: "nova/DOCS.md", line: "renamed Add-ons to Apps", reason: "explains the rename" },
  { rule: "add-on", file: "docs/reference/ha-doc-gate-2026-02-26.md", line: "Apps (formerly add-ons)", reason: "dated record; explains the rename" },
  { rule: "add-on", file: "docs/reference/ha-doc-gate-2026-02-26.md", line: "### 2) Apps / Add-ons", reason: "dated record; explains the rename" },
  { rule: "add-on", file: "docs/reference/hosted-demo.md", line: 'the "Demo add-on" device', reason: "Home Assistant's own demo device name, quoted" },
  { rule: "link-old-name", file: "cli/setup_cloud_mode.go", line: "used to be called Home Assistant Cloud", reason: "explains the rename" },
  { rule: "link-old-name", file: "cli/testdata/setup-golden/", line: "used to be called Home Assistant Cloud", reason: "explains the rename" },
  { rule: "link-old-name", file: "cli/cloud_origin.go", line: "canonical Nabu Casa origin", reason: "technical error about the provider's own domain" },
  { rule: "link-old-name", file: "cli/cloud_oauth_secret_linux.go", line: "HA NOVA Home Assistant Cloud authorization", reason: "label of keyring items already stored on users' computers" },
  // README edits wait for the 0.28 release-prep PR, which drops this entry (docs/work/next-release-body.md).
  { rule: "needs-computer", file: "README.md", line: "Helpers, dashboards, and files still need HA NOVA on a computer.", reason: "README changes only in release prep" },
];

const pages = [
  "cli/setupweb/strings.en.json",
  "cli/setupweb/index.html",
  ...readdirSync("cli/setupweb").filter((f) => f.endsWith(".js")).map((f) => `cli/setupweb/${f}`),
  "nova/src/http/handlers/nova-page-view.ts",
  "nova/src/http/handlers/nova-page.ts",
  "nova/src/http/handlers/home.ts",
];
const docs = [
  "README.md",
  ...readdirSync("docs/reference").filter((f) => f.endsWith(".md")).map((f) => `docs/reference/${f}`),
  "nova/DOCS.md",
  "nova/README.md",
  "nova/translations/en.yaml",
  ...readdirSync("cli/testdata/setup-golden").map((f) => `cli/testdata/setup-golden/${f}`),
  ...pages,
];
// Code comments aren't shown to anyone.
const textLines = (file: string) =>
  readFileSync(file, "utf8")
    .split("\n")
    .map((line) => (/\.(ts|js)$/.test(file) && /^\s*(\/\/|\/\*|\*)/.test(line) ? "" : line));

// Go: every string literal, on the line it starts (comments skipped). The
// typed code keeps its skill term "the one-time code" in text for the AI, so
// that rule reads only what a person sees: printHuman* calls and the say and
// phone text files.
const sayFiles = /^cli\/(say_lines|confirm_say|confirm_say_text|confirm_phone_text)\.go$/;
const personOnly = new Set(["one-time-code"]);
function goSurfaces(file: string) {
  const src = readFileSync(file, "utf8");
  const srcLines = src.split("\n");
  const all = srcLines.map(() => "");
  const person = srcLines.map(() => "");
  for (const m of src.matchAll(/\/\/[^\n]*|\/\*[\s\S]*?\*\/|"(?:[^"\\\n]|\\.)*"|`[^`]*`|'(?:[^'\\\n]|\\.)*'/g)) {
    if (!/^["`]/.test(m[0])) continue;
    const i = src.slice(0, m.index).split("\n").length - 1;
    const text = m[0].slice(1, -1).replace(/\\[nt]|\n/g, " ");
    all[i] += ` ${text}`;
    if (sayFiles.test(file) || /\bprintHuman\w*\(/.test(`${srcLines[i - 1] ?? ""} ${srcLines[i]}`)) person[i] += ` ${text}`;
  }
  return [
    { file, lines: all, skip: (id: string) => personOnly.has(id) },
    { file, lines: person, skip: (id: string) => !personOnly.has(id) },
  ];
}
const goFiles = readdirSync("cli")
  .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
  .map((f) => `cli/${f}`);

const sha = (word: string) => createHash("sha256").update(word).digest("hex");

// Hits that start on `cur`; the neighbour lines catch a phrase wrapped across lines.
function hits(rule: Rule, prev: string, cur: string, next: string): string[] {
  if (rule.sha256) {
    return (cur.toLowerCase().match(new RegExp(rule.words!, "g")) ?? []).flatMap((run) => {
      const parts = run.split(/[-_]/);
      return parts.flatMap((_, a) => parts.slice(a).map((__, b) => parts.slice(a, a + b + 1).join("-"))).filter((w) => rule.sha256!.includes(sha(w)));
    });
  }
  let text = `${prev} ${cur} ${next}`;
  if (rule.quotedOk) text = text.replace(/“[^”]*”/g, (q) => " ".repeat(q.length));
  const from = prev.length + 1;
  return [...text.matchAll(new RegExp(rule.pattern!, `g${rule.flags ?? ""}`))]
    .filter((m) => m.index >= from && m.index < from + cur.length)
    .map((m) => m[0]);
}

const surfaces: { file: string; lines: string[]; skip?: (id: string) => boolean }[] = [
  ...docs.map((file) => ({ file, lines: textLines(file) })),
  ...goFiles.flatMap(goSurfaces),
];
const used = new Set<number>();
const found = new Map(
  rules.map((rule) => {
    const out: string[] = [];
    for (const { file, lines, skip } of surfaces) {
      if ((rule.scope === "pages" && !pages.includes(file)) || skip?.(rule.id)) continue;
      lines.forEach((line, i) => {
        for (const hit of hits(rule, lines[i - 1] ?? "", line, lines[i + 1] ?? "")) {
          const ok = allow.findIndex((a) => a.rule === rule.id && (a.file === file || (a.file.endsWith("/") && file.startsWith(a.file))) && line.includes(a.line));
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

  it("catches the old chat-app limits, not the computer-only line of the chat skill header", () => {
    const hit = (id: string, text: string) => {
      const rule = rules.find((r) => r.id === id)!;
      return new RegExp(rule.pattern!, rule.flags).test(text);
    };
    for (const old of ["Helpers, dashboards, and files still need HA NOVA on a computer.", "That still needs HA NOVA on a computer."]) expect(hit("needs-computer", old)).toBe(true);
    for (const old of ["Anything not mapped here needs a client with a shell and the HA NOVA CLI", "its writes need a shell client with the HA NOVA CLI"]) expect(hit("shell-client", old)).toBe(true);
    const header = readFileSync("cli/mcp_skill.go", "utf8");
    expect(header).toContain("this sets up HA NOVA on a computer; tell the user to run it");
    for (const id of ["needs-computer", "shell-client"]) expect(hit(id, header)).toBe(false);
  });

  it("has no stale allow-list entry", () => {
    expect(allow.filter((_, i) => !used.has(i))).toEqual([]);
  });
});
