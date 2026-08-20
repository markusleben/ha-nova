// tests/skills/pack-power-contract.test.ts
// Power-lane pack items from #529 and #520.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

const context = flat(read("skills/ha-nova/SKILL.md"));
const bulk = flat(read("skills/ha-nova/bulk-patterns.md"));
const relayReady = flat(read("skills/fallback/relay-ready.md"));
const mqtt = flat(read("skills/mqtt/SKILL.md"));

describe("P2-08 MQTT ghost cleanup routing (#529)", () => {
  it("routes the broker-side cleanup from maintenance to mqtt", () => {
    const maintenance = flat(read("skills/maintenance/SKILL.md"));
    expect(maintenance).toContain(
      "MQTT ghost entities recreated by a retained discovery message — the broker-side cleanup is `ha-nova:mqtt`",
    );
    expect(maintenance).toContain(
      "registry removal here only sticks after that topic is cleared",
    );
  });

  it("names maintenance as the registry-side sibling in mqtt", () => {
    expect(mqtt).toContain(
      "the registry-side sibling cleanup in `ha-nova:maintenance`",
    );
  });
});

describe("P2-09 organize bulk seam (#529)", () => {
  it("wires selector-based metadata updates to bulk-patterns", () => {
    const organize = flat(read("skills/organize/SKILL.md"));
    expect(organize).toContain("## Bulk Target Resolution");
    expect(organize).toContain(
      "selected by prefix, domain, area, or label",
    );
    expect(organize).toContain(
      "resolve the shortlist per `skills/ha-nova/bulk-patterns.md` (Selector Semantics + Discovery Pipeline)",
    );
    expect(organize).toContain(
      "binds to that exact saved id list — never to the raw selector",
    );
  });
});

describe("P2-04 stale automation audit (#529)", () => {
  it("defines the one-pull inventory outside the 5-cap", () => {
    expect(bulk).toContain("## Stale Automation Audit (explicit request only)");
    expect(bulk).toContain("one `GET /api/states` pull filtered to `automation.*`");
    expect(bulk).toContain("Sort by `attributes.last_triggered` ascending");
    expect(bulk).toContain('`null` means never triggered — report it as "never"');
    expect(bulk).toContain("Report as a List Frame with exact counts");
    // Read-only inventory: the cap exemption must be stated, not implied.
    expect(bulk).toContain(
      "no per-item review runs, so the 5-target audit cap does NOT apply",
    );
  });
});

describe("P2-07 bulk continuation cursor (#529)", () => {
  it("freezes the selector and makes the ledger the cursor", () => {
    expect(bulk).toContain("### Continuation Rounds");
    expect(bulk).toContain(
      "re-runs the SAME frozen selector from round 1",
    );
    expect(bulk).toContain(
      "the completion ledger from earlier rounds is the cursor",
    );
    expect(bulk).toContain(
      "is a NEW task with a fresh shortlist — not a continuation",
    );
  });
});

describe("P2-05 template live render loop (#529)", () => {
  it("pins the pre-save render loop including dashboard fields", () => {
    const tg = flat(read("skills/ha-nova/template-guidelines.md"));
    expect(tg).toContain("## Live Render Loop (pre-save iteration)");
    expect(tg).toContain("POST --path /api/template");
    expect(tg).toContain("the rendered value comes back in `.data.body`");
    // 2026-08-19 issue comment: persisted Jinja outside Template helpers too.
    expect(tg).toContain(
      "dashboard templated fields (card titles, segment and visibility expressions)",
    );
    expect(tg).toContain("Surface render errors verbatim");
    expect(tg).toContain("instead of changing any real sensor state");
    expect(tg).toContain("The loop ends BEFORE the write preview");
  });
});

describe("P2-06 recorder routing (#529)", () => {
  it("gives yaml-config the recorder: scope line", () => {
    const yaml = flat(read("skills/yaml-config/SKILL.md"));
    expect(yaml).toContain(
      "`recorder:` configuration blocks (include/exclude filters, `purge_keep_days`)",
    );
    expect(yaml).toContain("recorder is not reloadable, a restart applies it");
  });

  it("routes the recorder edit in the dispatch table", () => {
    expect(context).toContain(
      "| edit `recorder:` include/exclude filters or `purge_keep_days` (YAML) | `ha-nova:yaml-config` |",
    );
    // Growth triage was removed on purpose (review batch): HA exposes no
    // per-entity DB-size API, so the advertised capability could not be served.
    expect(context).not.toContain("dominate recorder growth");
    expect(flat(read("skills/maintenance/SKILL.md"))).not.toContain(
      "recorder growth triage",
    );
  });
});

describe("P2-10 advanced-pattern trio (#529)", () => {
  const patterns = read("skills/ha-nova/automation-patterns.md");

  it("ships the manual-override window on a timer helper flag", () => {
    expect(patterns).toContain("### Manual-Override Window");
    expect(patterns).toContain("trigger.to_state.context.parent_id is none");
    expect(patterns).toContain("entity_id: timer.hallway_override");
    expect(patterns).toContain("state: idle");
  });

  it("ships the rate-limited notification on a timestamp helper", () => {
    expect(patterns).toContain("### Rate-Limited Notification");
    expect(patterns).toContain(
      "as_timestamp(now()) - as_timestamp(states('input_datetime.last_leak_alert'), 0) > 3600",
    );
    expect(patterns).toContain("input_datetime.set_datetime");
  });

  it("ships the presence-simulation loop with HA-side randomness", () => {
    expect(patterns).toContain("### Presence-Simulation Loop");
    expect(patterns).toContain("evaluated by Home Assistant at each run");
    expect(patterns).toContain("never pre-render a random value into the stored config");
    expect(patterns).toContain("{{ range(0, 25) | random }}");
    // Review batch: after: sunset alone is false from midnight to sunrise.
    expect(patterns).toContain("pair it with `before: sunrise`");
  });

  it("ships the two issue-named patterns in the correct shape", () => {
    expect(patterns).toContain("### Stale-Sensor Watchdog");
    expect(patterns).toContain("NOT via a `now()` template trigger");
    expect(patterns).toContain("### Native Adaptive Lighting");
    expect(patterns).toContain("ONLY while no manual override window is active");
  });
});

describe("C1-06 Z2M/ZHA read-only observability (#520)", () => {
  it("documents Z2M bridge topics as a bounded read in mqtt", () => {
    expect(mqtt).toContain("## Zigbee2MQTT Bridge Observability (read-only)");
    expect(mqtt).toContain("retained `zigbee2mqtt/bridge/...` topics");
    expect(mqtt).toContain("`retain: true` IS the answer");
    expect(mqtt).toContain("Bridge WRITES (rename, permit_join) are not covered here");
  });

  it("adds the read-only radio status lane to fallback without pinning unverified names", () => {
    expect(relayReady).toContain("### Zigbee / Z-Wave Network Status -- RELAY-READY");
    expect(relayReady).toContain('{"type":"zha/devices"}');
    expect(relayReady).toContain('{"type":"zwave_js/network_status","entry_id":"<entry_id>"}');
    // No repo reference pins these WS names — the section must say so.
    expect(relayReady).toContain(
      "pinned by no repo reference — verify them live before first use",
    );
    expect(flat(read("skills/fallback/SKILL.md"))).toContain(
      "| Zigbee / Z-Wave network status (read-only device list / network reads) | Relay-Ready | this skill; Z2M bridge topics: `mqtt` |",
    );
  });
});

describe("C1-02 bounded-capture generalization (#520)", () => {
  it("generalizes the button/remote/tag question in Bounded Event Capture", () => {
    expect(relayReady).toContain(
      'the generic answer to "what does my button / remote / tag fire?"',
    );
    expect(relayReady).toContain(
      "report the captured event TYPES together with their data keys",
    );
    expect(relayReady).toContain("`tag_scanned`");
    expect(relayReady).toContain("Never infer an event you did not capture");
  });

  it("routes the intent from the dispatch table", () => {
    expect(context).toContain(
      '| "what event does my button/remote/tag fire?" — capture it in a bounded window | `ha-nova:fallback` (Bounded Event Capture); MQTT devices: `ha-nova:mqtt` |',
    );
  });
});
