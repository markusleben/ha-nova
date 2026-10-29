package main

import (
	"strings"
	"testing"
)

// Chat apps (docs/work/2026-10-29-batch-confirmation-spec.md §7): ha_plan
// kind batch with plans, and ha_apply of a batch through a form and
// through ha-nova serve's phone job. The admin check: TestServeConfigPlansNeedAnAdministrator.

func TestMCPPlanBatchArgv(t *testing.T) {
	var schema map[string]any
	for _, tool := range mcpTools {
		if tool.name == "ha_plan" {
			schema = tool.schema
		}
	}
	two := []string{"p_000000000001", "p_000000000002"}
	argv, err := planArgv(rawArgs(t, map[string]any{"kind": "batch", "plans": two}), &mcpTempFiles{})
	if err != nil || strings.Join(argv, " ") != "batch --plan=p_000000000001 --plan=p_000000000002" {
		t.Fatalf("argv %v err %v", argv, err)
	}
	for name, args := range map[string]map[string]any{
		"no plans":         {"kind": "batch"},
		"not a plan id":    {"kind": "batch", "plans": []string{"p_000000000001", "--server=x"}},
		"another argument": {"kind": "batch", "plans": two, "entity_ids": []string{"light.kitchen"}},
	} {
		if argv, err := planArgv(rawArgs(t, args), &mcpTempFiles{}); err == nil {
			t.Errorf("%s: argv %v", name, argv)
		}
	}
	for _, plans := range [][]string{two[:1], make([]string, batchMaxMembers+1)} {
		if err := checkArguments(schema, rawArgs(t, map[string]any{"kind": "batch", "plans": plans})); err == nil {
			t.Errorf("%d plans: the schema took them", len(plans))
		}
	}
	if !strings.Contains(mcpSkillHeader, "plan batch --plan <id> … → kind batch\n  with plans [<id>, …]") {
		t.Error("ha_skill's header doesn't map plan batch")
	}
}

// A form shows the batch's list with the code; after a failed copy it asks
// again saying no copies can be saved, and one answer sends all.
func TestMCPBatchForm(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	c := startMCP(t, h.paths)
	formsInit(c)
	planID := func(args map[string]any) string {
		t.Helper()
		plan, _ := envelopeOf(c.tool("ha_plan", args))["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("no plan for %v", args)
		}
		return plan["id"].(string)
	}
	ids := []string{planID(map[string]any{"kind": "write", "domain": "automation", "id": "a1", "draft": map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}}}),
		planID(map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"})}
	batched := c.tool("ha_plan", map[string]any{"kind": "batch", "plans": ids})
	id := envelopeOf(batched)["plan"].(map[string]any)["id"].(string)
	list := "I'm about to make these 2 changes in Home Assistant, in this order:\n1. Change the automation “Night mode”.\n2. Delete the automation “Kitchen motion old”.\n"
	if say, _ := envelopeOf(batched)["say"].(string); !strings.HasPrefix(say, list) || !strings.HasSuffix(say, " "+sayFormAsk) {
		t.Fatalf("ha_plan batch: %v", envelopeOf(batched))
	}
	code := storedCode(t, h, id)
	noCode(t, "ha_plan batch", batched, code)
	h.f.snapshotErr = true
	call := c.startCall(applyArgs(id, nil))
	formID, params, typed := c.form()
	if message := params["message"].(string); !strings.HasPrefix(message, list) || !strings.Contains(message, confirmT("en", "batch.copies")) {
		t.Fatalf("the form: %q", message)
	}
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	if failed := c.final(call); errCode(failed) != "SNAPSHOT_FAILED" || len(h.writes()) != 0 {
		t.Fatalf("snapshot failure: %v", failed)
	}
	call = c.startCall(applyArgs(id, map[string]any{"no_snapshot": true}))
	formID, params, typed = c.form()
	if message := params["message"].(string); !strings.HasPrefix(message, list) || strings.Contains(message, confirmT("en", "batch.copies")) ||
		!strings.Contains(message, "No copies could be saved, so once deleted they can't be restored.") {
		t.Fatalf("the no-copy form: %q", message)
	}
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	if applied := c.final(call); envelopeOf(applied)["status"] != "verified" || len(h.writes()) != 2 {
		t.Fatalf("applied: %v", applied)
	}
}

// ha-nova serve: the phone asks for the whole batch; the tap applies it,
// and the next ha_apply returns the ledger.
func TestServeBatchWaitsForThePhone(t *testing.T) {
	f := batchHome()
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint != "ws" || p["type"] != "config/auth/list" {
			return nil, false
		}
		return []map[string]any{{"id": userA, "is_active": true, "group_ids": []string{"system-admin"}}}, true
	}
	s := newServe(t, f, `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	planID := func(args map[string]any) string {
		t.Helper()
		plan, _ := s.tool(userA, "ha_plan", args)["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("no plan for %v", args)
		}
		return plan["id"].(string)
	}
	ids := []any{planID(map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")}),
		planID(map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"})}
	env := s.tool(userA, "ha_plan", map[string]any{"kind": "batch", "plans": ids})
	plan, _ := env["plan"].(map[string]any)
	if plan["channel"] != channelPhone || plan["code"] != nil || !strings.Contains(env["next_step"].(string), "a notification on their phone") {
		t.Fatalf("batch plan: %v", env)
	}
	id := plan["id"].(string)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" || p.sentCount() != 1 || len(s.writes()) != 0 {
		t.Fatalf("before the tap: %v", env)
	}
	ok, _ := p.buttons(t)
	p.events <- tap(ok, userA, "REMOTE")
	waitFor(t, "the batch after the tap", func() bool { return len(s.writes()) == 2 })
	waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
	env = s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
	result, _ := env["result"].(map[string]any)
	items, _ := result["items"].([]any)
	if env["status"] != "verified" || len(items) != 2 || items[1].(map[string]any)["status"] != "verified" {
		t.Fatalf("after the tap: %v", env)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sent[0]["title"] != "Change or delete 2 automations and scripts?" || p.sent[1]["title"] != "Done: Change or delete 2 automations and scripts" ||
		!strings.Contains(p.sent[0]["message"].(string), "Requested in your AI chat app.") {
		t.Fatalf("notifications %v", p.sent)
	}
}
