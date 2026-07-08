import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const todoSkill = readFileSync("skills/todo/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("todo contract", () => {
  it("gates item operations on the provider feature bitmask", () => {
    expect(todoSkill).toContain("## Feature Gate (critical)");
    expect(todoSkill).toContain("`supported_features`");
    // The bitmask rows agents need to decode provider capabilities.
    expect(todoSkill).toContain("| 16 | set due date |");
    expect(todoSkill).toContain("| 64 | set description |");
    expect(todoSkill).toContain("never fire the service anyway");
  });

  it("teaches the mandatory return_response query parameter (live-verified 400 without it)", () => {
    expect(todoSkill).toContain("?return_response");
    expect(todoSkill).toContain("without it HA returns 400");
    expect(todoSkill).toContain('.data.body.service_response["todo.<list>"].items');
    // Error taxonomy for the live-verified failure shapes.
    expect(todoSkill).toContain('`get_items` returning 400 "requires responses"');
    expect(todoSkill).toContain("Item-service 400/500: the item does not exist");
    // Cloud sync lag and the recurrence gap (users will ask; HA cannot).
    expect(todoSkill).toContain("sync lag, not failure");
    expect(todoSkill).toContain("never re-add");
    expect(todoSkill).toContain("HA has no recurring items");
  });

  it("prefers uids over summaries and pre-resolves before removal", () => {
    expect(todoSkill).toContain("always prefer `uid`");
    expect(todoSkill).toContain("removing a non-existent item is a raw HA error");
    expect(todoSkill).toContain("Read items first and resolve exact `uid`s");
    expect(todoSkill).toContain("ask before adding a duplicate");
    expect(todoSkill).toContain("`due_date` and `due_datetime` are mutually exclusive");
    // Reorder has no service — WS todo/item/move is the only path.
    expect(todoSkill).toContain('"type":"todo/item/move"');
    // Every item-write body carries the entity_id target explicitly —
    // entity services without a target are silent no-ops.
    expect(todoSkill).toContain('`/api/services/todo/update_item` with `{"entity_id":"todo.<list>","item":"<uid>", ...}`');
    expect(todoSkill).toContain('`/api/services/todo/remove_item` with `{"entity_id":"todo.<list>","item":[...]}`');
    expect(todoSkill).toContain('`/api/services/todo/remove_completed_items` with `{"entity_id":"todo.<list>"}`');
  });

  it("domain-gates list deletion so integration entries are never deleted as lists", () => {
    expect(todoSkill).toContain("**Domain gate:**");
    expect(todoSkill).toContain("proceed only if its `domain` is `local_todo`");
    expect(todoSkill).toContain("the entry is the WHOLE integration, not one list");
    expect(todoSkill).toContain("refuse and point to that integration's own management");
  });

  it("covers the Local To-do list lifecycle with token-gated irreversible deletion", () => {
    expect(todoSkill).toContain('{"handler":"local_todo"}');
    expect(todoSkill).toContain("`todo_list_name`");
    expect(todoSkill).toContain("registry lag: retry once");
    expect(todoSkill).toContain("destroys the list AND all its items irreversibly");
    expect(todoSkill).toContain("`confirm:<token>`");
    expect(todoSkill).toContain("proceed only when the user types it back exactly");
    expect(todoSkill).toContain("Resolve `entry_id` via the entity registry `config_entry_id`");
    expect(todoSkill).toContain("never derive the entity_id by slugifying the name");
    // Other providers configure lists in their own integration.
    expect(todoSkill).toContain("point there instead of guessing flows");
  });

  it("is wired into dispatch, capability map, availability table, and architecture doc", () => {
    expect(contextSkill).toContain("| show, add, complete, update, remove to-do or shopping-list items; create/delete to-do lists | `ha-nova:todo` |");
    expect(contextSkill).toContain('"Add milk to my shopping list"** → `ha-nova:todo`');
    expect(fallbackSkill).toContain("| To-do Lists (items + Local To-do lifecycle) | Covered | todo |");
    expect(writeSafety).toContain("| `todo` | preview + read-back verify | no (list delete irreversible) | re-add items; HA Backups for lists |");
    // The total sub-skill count is pinned once, in health-calendar-contract.
    expect(architectureDoc).toContain("todo/SKILL.md");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(todoSkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(todoSkill).toContain("skills/ha-nova/output-rules.md");
    expect(todoSkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
    expect(todoSkill).toContain("never raw JSON");
  });
});
