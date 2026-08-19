import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #594: one shared live-schema preflight contract for supported config/options
// flows. Previews must come from the LIVE form, nothing terminal is submitted
// before the bound confirmation, drift and validation errors are explicit
// stops, and terminal `create_entry` identity extraction knows the nested
// `result.entry_id` shape (the observed miss: `.data.body.entry_id` read null
// while the entry existed).

// Pins are whitespace-normalized so a reflow of the Markdown never breaks them.
const flat = (path: string) =>
  readFileSync(path, "utf8").replace(/\s+/g, " ");

const preflight = flat("skills/ha-nova/live-schema-preflight.md");
const helperSkill = flat("skills/helper/SKILL.md");
const integrationSkill = flat("skills/integration-setup/SKILL.md");
const relayReady = flat("skills/fallback/relay-ready.md");

describe("live-schema preflight contract (#594)", () => {
  it("scopes the contract to supported families and keeps the #493 boundary", () => {
    expect(preflight).toContain(
      "It applies ONLY to explicitly supported flow families",
    );
    expect(preflight).toContain(
      "never authorizes probing unfamiliar endpoints or arbitrary config flows",
    );
    expect(preflight).toContain("the fail-closed rule from #493");
    expect(preflight).toContain("Write-Probing Asymmetry");
    expect(preflight).toContain(
      "everything outside the supported families stays with `ha-nova:fallback`",
    );
    expect(preflight).toContain("Start only a known, allowlisted flow");
  });

  it("keeps the Relay dumb — no new endpoint, orchestration is skill-side", () => {
    expect(preflight).toContain("No new Relay endpoint exists for this");
    expect(preflight).toContain("the Relay stays dumb");
  });

  it("assembles previews from the live form and labels field provenance", () => {
    expect(preflight).toContain(
      "Read the LIVE response of every step: `data_schema`, suggested/default values (`description.suggested_value`), `step_id`, and `last_step`",
    );
    expect(preflight).toContain(
      "assembled from the live form, never from previously observed schemas",
    );
    expect(preflight).toContain(
      "labels which fields came from the live running flow and which remain unavailable",
    );
    expect(preflight).toContain("unavailable fields are named, never guessed");
  });

  it("stops before the terminal submit for one-step and multi-step flows", () => {
    expect(preflight).toContain("STOP before the terminal submit");
    expect(preflight).toContain(
      "One-step flows (`last_step: true` on the first form) therefore stop before their only submit",
    );
    expect(preflight).toContain(
      "multi-step flows stop after the last non-persisting step",
    );
    expect(preflight).toContain(
      "nothing terminal is sent before the bound confirmation",
    );
    // Pre-confirmation navigation is limited to proven non-persisting steps.
    expect(preflight).toContain(
      "ONLY through steps proven non-persisting and side-effect-free for that family",
    );
    expect(preflight).toContain(
      "persistence behavior is not documented for the family, stop before submitting it",
    );
  });

  it("binds confirmation to the live schema plus the exact terminal payload", () => {
    expect(preflight).toContain(
      "Confirmation binds to the live schema plus the exact terminal payload",
    );
    expect(preflight).toContain(
      "On cancel or expiry, abandon the transient flow without submitting the terminal step",
    );
  });

  it("treats validation errors and schema drift as explicit stops, never guess-or-retry", () => {
    expect(preflight).toContain("## Explicit stops (never guess-or-retry)");
    expect(preflight).toContain(
      "Validation errors: show the returned field errors and stop.",
    );
    expect(preflight).toContain("Schema drift between preview and submit");
    expect(preflight).toContain("enum options");
    expect(preflight).toContain("the old confirmation is void");
  });

  it("reconciles unknown outcomes before any retry", () => {
    expect(preflight).toContain(
      "Unknown outcomes (timeout, transport error, ambiguous terminal response)",
    );
    expect(preflight).toContain(
      "reconcile actual state via `config_entries/get` and flow progress BEFORE any retry",
    );
    expect(preflight).toContain("a blind retry can double-create");
  });

  it("normalizes terminal create_entry identity incl. nested result.entry_id", () => {
    // Shared envelope guidance, stated once.
    expect(preflight).toContain(
      "Relay `/core` responses carry the upstream payload under `.data.body`",
    );
    expect(preflight).toContain(
      "`/ws` responses carry it under `.data` directly",
    );
    // The observed miss from the issue.
    expect(preflight).toContain("`.data.body.result.entry_id`");
    expect(preflight).toContain(
      "NOT at `.data.body.entry_id` — reading the flat path returns null even though the entry was created",
    );
    // Constrained before/after diff stays the fallback.
    expect(preflight).toContain(
      "the constrained before/after `config_entries/get` diff is the fallback",
    );
    expect(preflight).toContain(
      "exactly one new `entry_id` appeared and its metadata is consistent with the request",
    );
    expect(preflight).toContain("fail loud as ambiguous verification");
  });

  it("is wired from all three owning surfaces", () => {
    expect(helperSkill).toContain(
      "Config-entry flow orchestration follows the shared contract in `skills/ha-nova/live-schema-preflight.md`",
    );
    // The integration-setup pointer ADDS the navigation and terminal-stop
    // rules on top of its existing every-response-is-authoritative stance.
    expect(integrationSkill).toContain(
      "follows the shared `skills/ha-nova/live-schema-preflight.md` contract, which additionally binds pre-confirmation navigation",
    );
    expect(integrationSkill).toContain("Treat every response as authoritative");
    // Fallback's experimental lane stays outside the contract, fail-closed.
    expect(relayReady).toContain(
      "The supported families' orchestration contract is `skills/ha-nova/live-schema-preflight.md`",
    );
    expect(relayReady).toContain(
      "this experimental lane stays outside it and remains fail-closed per #493",
    );
  });
});
