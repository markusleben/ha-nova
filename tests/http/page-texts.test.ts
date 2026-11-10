// The approval page's words (page-texts.ts): checked, logged when unusable,
// kept a minute per name, one load shared, a failed load not kept.
import { describe, expect, it } from "vitest";

import { PAGE_KEYS, pageTexts } from "../../nova/src/http/page-texts.js";

const TEXTS = Object.fromEntries(PAGE_KEYS.map((key) => [key, key + " <x> {count}"]));

function source(answer: (name: string) => unknown = () => ({ lang: "de", texts: TEXTS })) {
  let now = 0;
  const loads: string[] = [];
  const warnings: unknown[] = [];
  const pending: Array<() => void> = [];
  const texts = pageTexts(
    (name) => {
      loads.push(name);
      return new Promise((resolve) => pending.push(() => resolve(answer(name))));
    },
    () => now,
    { warn: (message, context) => void warnings.push([message, context]) },
  );
  const settle = async () => {
    pending.splice(0).forEach((resolve) => resolve());
    await new Promise((resolve) => setImmediate(resolve));
  };
  return { texts, loads, warnings, settle, tick: (ms: number) => (now += ms) };
}

describe("page texts", () => {
  it("say a text escaped, with its number", async () => {
    const s = source();
    const t = s.texts("");
    await s.settle();
    const words = await t;
    expect(words?.lang).toBe("de");
    expect(words?.say("page.more", { count: 2 })).toBe("page.more &lt;x&gt; 2");
  });

  it("share one load while it runs and keep it a minute per name", async () => {
    const s = source();
    const [a, b] = [s.texts("Anna"), s.texts("Anna")];
    s.texts("");
    await s.settle();
    expect(await a).toBe(await b);
    expect(s.loads).toEqual(["Anna", ""]);
    s.tick(59_999);
    void s.texts("Anna");
    expect(s.loads).toEqual(["Anna", ""]);
    s.tick(1);
    void s.texts("Anna");
    expect(s.loads).toEqual(["Anna", "", "Anna"]);
  });

  it("keep at most 16 names, the oldest going first", async () => {
    const s = source();
    for (let i = 0; i <= 16; i++) {
      void s.texts("user " + i);
    }
    await s.settle();
    void s.texts("user 16");
    void s.texts("user 0");
    expect(s.loads.filter((name) => name === "user 0")).toHaveLength(2);
    expect(s.loads.filter((name) => name === "user 16")).toHaveLength(1);
  });

  it("never keep a failed load", async () => {
    let fail = true;
    const s = source(() => (fail ? null : { lang: "en", texts: TEXTS }));
    const first = s.texts("");
    await s.settle();
    expect(await first).toBeNull();
    fail = false;
    const second = s.texts("");
    await s.settle();
    expect(await second).not.toBeNull();
    expect(s.loads).toEqual(["", ""]);
  });

  it("refuse and log a missing, empty, or non-text key, and an unusable language", async () => {
    const missing = { ...TEXTS };
    delete missing["page.back"];
    const cases: [unknown, Record<string, unknown>][] = [
      [{ lang: "en", texts: missing }, { key: "page.back" }],
      [{ lang: "en", texts: { ...TEXTS, "page.open": "" } }, { key: "page.open" }],
      [{ lang: "en", texts: { ...TEXTS, "page.approve": 1 } }, { key: "page.approve" }],
      [{ lang: "en", texts: null }, { key: "page.title" }],
      [{ lang: 'en"><script>', texts: TEXTS }, { lang: 'en"><script>' }],
      [{ texts: TEXTS }, { lang: "undefined" }],
    ];
    for (const [answer, context] of cases) {
      const s = source(() => answer);
      const t = s.texts("");
      await s.settle();
      expect(await t).toBeNull();
      expect(s.warnings).toEqual([[expect.any(String), context]]);
    }
  });
});
