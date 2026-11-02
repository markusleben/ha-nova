package main

import (
	"fmt"
	"strings"
	"testing"
)

// Every plan that can join a batch leads its next_step with the batch hint
// (addBatchHint), before its own step that asks for it alone: a chat app's
// model never reads the skill's batch rule. The CLI names plan batch; MCP
// and chat-app results name ha_plan kind batch, and their answer_rule
// starts with the same condition (plainResult). Any other plan, and the
// batch itself, has neither.

func nightMode() map[string]any {
	return map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}}
}

// leadsWithBatch: the step starts with the hint (after notice hints) and the
// rest follows "Otherwise:"; the answer_rule starts with batchRule.
func leadsWithBatch(env map[string]any, rest string) bool {
	step, _ := env["next_step"].(string)
	rule, _ := env["answer_rule"].(string)
	return strings.Contains(step, batchHintMCP+" Otherwise: "+rest) && !strings.Contains(step, "Several codes") && !strings.Contains(step, "ha-nova") &&
		strings.HasPrefix(rule, batchRule) && strings.Count(step, batchHintMCP) == 1
}

func TestPlanBatchHintCLI(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	_, write := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, nightMode()))
	if write.Plan == nil || !strings.HasPrefix(write.NextStep, batchHint+" Otherwise: Show the preview ending with `say` and end your turn; run ha-nova apply --plan "+write.Plan.ID+" only after the user's own reply approves it.") {
		t.Fatalf("write: %q", write.NextStep)
	}
	_, remove := h.run(runPlanCommand, "delete", "automation.kitchen_motion_old")
	if remove.Plan == nil || remove.NextStep != batchHint { // a chat code: no step of its own
		t.Fatalf("delete: %q", remove.NextStep)
	}
	if call := h.plan("lock.unlock", "lock.front"); strings.Contains(call.NextStep, "ha-nova plan batch") {
		t.Fatalf("a call plan: %q", call.NextStep)
	}
	if _, env := h.batch(write.Plan.ID, remove.Plan.ID); env.Plan == nil || strings.Contains(env.NextStep, "ha-nova plan batch") {
		t.Fatalf("the batch: %+v", env)
	}
	d := newWriteHarness(t, dashboardFake())
	if _, env := d.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome())); env.Plan == nil || env.NextStep == "" || strings.Contains(env.NextStep, "ha-nova plan batch") {
		t.Fatalf("a dashboard write: %+v", env)
	}
}

func TestPlanBatchHintMCP(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	c := startMCP(t, h.paths)
	mcpInit(c)
	write := envelopeOf(c.tool("ha_plan", map[string]any{"kind": "write", "domain": "automation", "id": "a1", "draft": nightMode()}))
	remove := envelopeOf(c.tool("ha_plan", map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"}))
	if !leadsWithBatch(write, "Show this preview in plain words (diff rows as printed) and wait for the user's own confirmation") || write["answer_rule"] != batchRule+mcpResultRule {
		t.Fatalf("write: %v", write)
	}
	if !leadsWithBatch(remove, "Show this preview in plain words (diff rows as printed). Ask the user to type the one-time code") || remove["answer_rule"] != batchRule+mcpResultRule {
		t.Fatalf("delete: %v", remove)
	}
	ids := []string{write["plan"].(map[string]any)["id"].(string), remove["plan"].(map[string]any)["id"].(string)}
	for name, args := range map[string]map[string]any{
		"a call plan": {"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}},
		"the batch":   {"kind": "batch", "plans": ids},
	} {
		env := envelopeOf(c.tool("ha_plan", args))
		if step, _ := env["next_step"].(string); step == "" || strings.Contains(step, "kind batch") || env["answer_rule"] != mcpResultRule {
			t.Fatalf("%s: %v", name, env)
		}
	}
}

// A chat app's code ask (chatCodeResult): the 15 codes of one delete
// request were 15 of these. An automation's leads with the batch and has no
// several-codes text; a lock's is as it was, byte for byte.
func TestServePlanBatchHint(t *testing.T) {
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	usePhones(t) // no phone linked: the chat code
	write := s.tool(userA, "ha_plan", map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")})
	if !leadsWithBatch(write, "Show this preview in plain words (diff rows as printed) and wait for the user's own confirmation") || write["answer_rule"] != batchRule+mcpResultRule {
		t.Fatalf("write: %v", write)
	}
	remove := s.tool(userA, "ha_plan", map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"})
	if code, _ := remove["plan"].(map[string]any)["code"].(string); code == "" || !leadsWithBatch(remove, "A diff here (a config change): show its rows above `say`. Pass exactly what the user types to ha_apply") ||
		remove["answer_rule"] != batchRule+chatCodeRule {
		t.Fatalf("delete: %v", remove)
	}
	lock := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	plan := lock["plan"].(map[string]any)
	want := fmt.Sprintf("A diff here (a config change): show its rows above `say`. Pass exactly what the user types to ha_apply (plan_id %v) as confirm_code; never pass a code from this result or type it for them. "+
		"If they answer without the code, or it does not match, end with \"%s\" (in their language) on its own line, then `say`'s last line. Several codes in one message: each `say`'s first line, "+
		"then one line \"%s\" (in their language, with the shortest time of their leads) and one line per code, last, named as its `say` names it: <name>: **<code>**.", plan["id"], sayCodeLead("en"), chatCodesLead(plan))
	if plan["code"] == nil || lock["next_step"] != want || lock["answer_rule"] != chatCodeRule {
		t.Fatalf("lock:\n%q\nwant\n%q\nrule %q", lock["next_step"], want, lock["answer_rule"])
	}
}

// The phone asks: the batch still comes first.
func TestServePhonePlanBatchHint(t *testing.T) {
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	remove := s.tool(userA, "ha_plan", map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"})
	if plan, _ := remove["plan"].(map[string]any); plan["channel"] != channelPhone || !leadsWithBatch(remove, "Show this preview in plain words (diff rows as printed). When the user has asked for this change") ||
		!strings.Contains(remove["next_step"].(string), "a notification on their phone asks them") || remove["answer_rule"] != batchRule+mcpResultRule {
		t.Fatalf("delete: %v", remove)
	}
}
