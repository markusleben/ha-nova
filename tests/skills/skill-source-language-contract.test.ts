// tests/skills/skill-source-language-contract.test.ts
//
// AGENTS.md: skill SOURCES are 100% English. The product is multilingual at
// RUNTIME — NOVA answers in the user's language — but a German example baked
// into the source drags the output language with it, so an English-speaking
// user gets German fragments. Split out of the everyday-convenience contract,
// which had grown past the repository's ~400-line ceiling.
import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "fs";

// AGENTS.md: skill files are 100% English. Localisation happens at runtime,
// never in the source an agent pattern-matches against.
//
// A "bad word" list fails on the first word nobody thought of, so this keys on
// the closed class that actually makes a sentence German — function words —
// plus the household nouns these skills use as examples. Every entry was
// checked to be a non-word in English; an ambiguity tier ("die", "an", "in",
// "so", "was") was tried and dropped, because ordinary English prose carries
// three of those per sentence and it only produced noise. Longer inflections
// come first so the reported token is the whole word.
//
// The alternation is spelled out rather than using `\w*`: inside a template
// literal `\w` is not a recognised escape and collapses to a bare "w", which
// silently narrowed `kein\w*` to `keinw*`.
//
// No lexical detector is exhaustive. The bar is realistic German prose in a
// skill file, not every possible German sentence — `germanFixtures` below is
// the executable statement of that bar.
const L = "[A-Za-zÄÖÜäöüß]";
const GERMAN_WORDS = new RegExp(
  `(?<!${L})(der|das|den|dem|des|eine|einen|einem|einer|eines|ein|ist|sind|wird|werden|nicht|keine|keinen|keinem|keiner|keines|kein|und|oder|aber|für|von|zum|zur|vom|beim|ins|im|aus|nach|über|unter|seit|bis|ohne|gegen|durch|wieder|ich|du|wir|es|sich|mich|dich|euch|sein|ihre|unser|dieser|diese|dieses|diesem|diesen|jeder|jedes|jedem|jeden|jede|welcher|welches|welchem|welchen|welche|dass|weil|wenn|wie|wer|warum|hier|dort|jetzt|dann|noch|schon|auch|nur|sehr|mehr|viel|wenig|alle|etwas|nichts|immer|bitte|heute|gestern|zwei|drei|vier|mach|zeig|schalte|dimme|soll|kann|muss|haben|gibt|Lampe|Licht|Fenster|Tür|Küche|Wohnzimmer|Schlafzimmer|Stehlampe|Heizung|Rollladen|Steckdose)(?!${L})`,
  "i",
);
// A LOWERCASE word carrying ß or an umlaut is German orthography: English has
// no such word. Capitalized ones are nouns or PROPER nouns, which AGENTS.md
// explicitly permits — `München` and attribution names must pass, so they are
// left to the word list, which already carries `Küche` and `Tür`. The
// lookarounds spell out "letter" because JS \b is ASCII-defined and would see
// a boundary inside `München`, matching `ünchen`.
const GERMAN_ORTHOGRAPHY = new RegExp(
  `(?<!${L})(?:[a-zäöüß]*[ßäöü][a-zäöüß]{2,}|[a-zäöüß]{2,}[ßäöü][a-zäöüß]*)(?!${L})`,
);
// Normalisation belongs HERE, not at the call site: a scanner that strips
// differently from what the fixtures below exercise is untested by them.
const detectGerman = (text: string): string | null => {
  const prose = text
    // Entity ids may be German — a real household names things in its own
    // language. Only DOTTED identifiers are exempt: `light.wohnzimmer` is an
    // id, `` `bitte` `` is German wearing backticks.
    .replace(/`[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+`/g, " ")
    // A LONE all-caps token amid normal prose is an acronym ("ES modules",
    // "over IM", "DES"); a RUN of them is shouted German ("DAS LICHT IST AN").
    // Dropping the lone ones kills the homograph class outright instead of
    // deleting one colliding entry per review round, while keeping runs means
    // a German heading is still caught.
    .replace(/\b[A-Z]{2,}\b/g, (match, offset: number, full: string) => {
      const runsInto = /^[^A-Za-z]*\b[A-Z]{2,}\b/.test(
        full.slice(offset + match.length),
      );
      const runsFrom = /\b[A-Z]{2,}\b[^A-Za-z]*$/.test(full.slice(0, offset));
      return runsInto || runsFrom ? match : " ";
    });
  return GERMAN_WORDS.exec(prose)?.[0] ?? GERMAN_ORTHOGRAPHY.exec(prose)?.[0] ?? null;
};

// Every row is a defect this detector was widened or narrowed to handle, so a
// future edit that reintroduces one fails here instead of in a skill file.
const germanFixtures: Array<[boolean, string]> = [
  [true, "kein Geraet gefunden"],
  [true, "keine Aenderung noetig"],
  [true, "Es regnet seit drei Tagen."],
  [true, "Guten Morgen, das Licht ist an."],
  [true, "Die Lampe im Wohnzimmer"],
  [true, "Der Schalter ist über der Tür."],
  [true, "Rollladen wieder oeffnen ohne Verzoegerung"],
  [true, "Sind alle Fenster zu?"],
  // No word-list marker in these three, so they exercise the orthography path
  // alone. One per alternation: a leading umlaut (nothing before it) reaches
  // only the first, a trailing one only the second.
  [true, "Fensterkontakt öffnen"],
  [true, "Zeitplan wurde geändert"],
  [true, "Heizungsventil schließt zu frueh"],
  [false, "Verified on an instance in München by Jürgen Müller."],
  [false, "Attribution: Lämmle, Grüßner, Öztürk."],
  [false, "Turn on the kitchen light"],
  [false, "Diesel generators and war rooms are out of scope."],
  [false, "The device is unavailable, so the call may still fail."],
  [false, "Every tag in the tags list keeps its own identifier."],
  [false, "A device that wears two hats needs two entries."],
  // All-caps acronyms collide with short function words; the scanner
  // strips them before matching, so these must stay silent.
  [false, "ES modules load lazily; send over IM if DES is required."],
  // …but a RUN of all-caps words is shouted German, not acronyms.
  [true, "DAS LICHT IST AN"],
];

describe("skill sources stay English (AGENTS.md)", () => {
  it("flags realistic German prose without rejecting proper nouns", () => {
    for (const [isGerman, sample] of germanFixtures) {
      expect(Boolean(detectGerman(sample)), sample).toBe(isGerman);
    }
  });

  it("keeps every skill source free of German", () => {
    const offenders: string[] = [];
    const walk = (dir: string): void => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const path = `${dir}/${entry.name}`;
        if (entry.isDirectory()) walk(path);
        else if (entry.name.endsWith(".md")) {
          readFileSync(path, "utf-8")
            .split("\n")
            .forEach((line, i) => {
              const hit = detectGerman(line);
              if (hit) offenders.push(`${path}:${i + 1} "${hit}" in ${line.trim().slice(0, 80)}`);
            });
        }
      }
    };
    walk("skills");
    expect(offenders, `German in skill sources:\n  ${offenders.join("\n  ")}`).toEqual([]);
  });
});
