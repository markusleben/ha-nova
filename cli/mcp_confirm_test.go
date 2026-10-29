package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The human types the code in a confirmation form, never the model
// (docs/work/2026-10-16-mcp-human-confirmation-spec.md).

var formCode = regexp.MustCompile(`Type ([0-9]{4}) to confirm\.$`)

var planIDText = regexp.MustCompile(`p_[0-9a-f]{12}`)

// wrongNumber is wrongCode as a JSON number.
func wrongNumber(code string) float64 {
	n, _ := strconv.Atoi(wrongCode(code))
	return float64(n)
}

// wrongCode is four digits that are not code.
func wrongCode(code string) string {
	n, _ := strconv.Atoi(code)
	return fmt.Sprintf("%04d", (n+1)%10000)
}

// recvAny reads one line the server wrote: a response, or its own request
// or notification.
func (c *mcpClient) recvAny() map[string]any {
	c.t.Helper()
	line, err := c.out.ReadString('\n')
	if err != nil {
		c.t.Fatalf("no message: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil || msg["jsonrpc"] != "2.0" {
		c.t.Fatalf("not a JSON-RPC line: %q", line)
	}
	return msg
}

// startCall sends a tools/call without waiting; it returns the request id.
func (c *mcpClient) startCall(params map[string]any) float64 {
	c.t.Helper()
	c.id++
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": "tools/call", "params": params})
	c.send(string(raw))
	return float64(c.id)
}

// form reads the elicitation/create the server sends inside a tools/call and
// returns its id, its params, and the code the human reads in it.
func (c *mcpClient) form() (string, map[string]any, string) {
	c.t.Helper()
	msg := c.recvAny()
	params, _ := msg["params"].(map[string]any)
	id, _ := msg["id"].(string)
	message, _ := params["message"].(string)
	m := formCode.FindStringSubmatch(message)
	if msg["method"] != "elicitation/create" || id == "" || m == nil {
		c.t.Fatalf("want a confirmation form, got %v", msg)
	}
	return id, params, m[1]
}

// answer sends the client's response to the server's request id.
func (c *mcpClient) answer(id string, result any) {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	c.send(string(raw))
}

// final reads the tools/call response with the given id.
func (c *mcpClient) final(id float64) map[string]any {
	c.t.Helper()
	msg := c.recvAny()
	result, _ := msg["result"].(map[string]any)
	if msg["id"] != id || result == nil {
		c.t.Fatalf("want the result of call %v, got %v", id, msg)
	}
	return result
}

func formsInit(c *mcpClient) {
	c.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"elicitation": map[string]any{}}})
}

func applyArgs(planID string, extra map[string]any) map[string]any {
	args := map[string]any{"plan_id": planID}
	for k, v := range extra {
		args[k] = v
	}
	return map[string]any{"name": "ha_apply", "arguments": args}
}

func envelopeOf(result map[string]any) map[string]any {
	envelope, _ := result["structuredContent"].(map[string]any)
	return envelope
}

func errCode(result map[string]any) any {
	errInfo, _ := envelopeOf(result)["error"].(map[string]any)
	return errInfo["code"]
}

// storedCode is the plan's code as the CLI derives it with the test Relay
// credential (the plan file alone does not yield it).
func storedCode(t *testing.T, h *writeHarness, id string) string {
	t.Helper()
	plan, err := readPlanFile(filepath.Join(planDir(h.paths), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return confirmCode("test-token", planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce), plan.Channel, plan.ExpiresAt)
}

// noCode: the result carries no code field and no text shows the code (a
// hash or timestamp may contain the same four digits by chance).
func noCode(t *testing.T, what string, result map[string]any, code string) {
	t.Helper()
	raw, _ := json.Marshal(result)
	if !fourDigits.MatchString(code) || strings.Contains(string(raw), `"code":"`+code) {
		t.Fatalf("%s shows the code %q: %s", what, code, raw)
	}
	envelope := envelopeOf(result)
	errInfo, _ := envelope["error"].(map[string]any)
	shown := regexp.MustCompile(`(^|[^0-9])` + code + `([^0-9]|$)`)
	for _, text := range []any{envelope["say"], envelope["next_step"], errInfo["message"], errInfo["next"]} {
		if text, _ := text.(string); shown.MatchString(planIDText.ReplaceAllString(text, "")) { // an id may hold the digits by chance
			t.Fatalf("%s shows the code %q: %q", what, code, text)
		}
	}
}

func TestFormElicitationCapability(t *testing.T) {
	for raw, want := range map[string]bool{`{}`: false, `{"elicitation":{}}`: true, `{"elicitation":{"form":{}}}`: true,
		`{"elicitation":{"form":{},"url":{}}}`: true, `{"elicitation":{"url":{}}}`: false, `{"elicitation":true}`: false} {
		var caps map[string]any
		_ = json.Unmarshal([]byte(raw), &caps)
		if formElicitation(caps) != want {
			t.Fatalf("%s: want %v", raw, want)
		}
	}
}

// Legacy era: the code never reaches the model, ha_apply asks the human in a
// form, one send, and no second use of the plan.
func TestMCPFormConfirmsTypedPlanLegacy(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	formsInit(c)
	planned := c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	envelope := envelopeOf(planned)
	plan := envelope["plan"].(map[string]any)
	id := plan["id"].(string)
	code := storedCode(t, h, id)
	say, _ := envelope["say"].(string)
	step, _ := envelope["next_step"].(string)
	if planned["isError"] != false || envelope["status"] != "needs_confirmation" || plan["confirm"] != "code" || plan["code"] != nil ||
		!strings.HasSuffix(say, sayFormAsk("en")) || !strings.Contains(step, "call ha_apply with plan_id "+id+" and no confirm_code") || !strings.Contains(step, "Never ask for it in chat") {
		t.Fatalf("typed plan with forms: %v", planned)
	}
	noCode(t, "ha_call", planned, code)

	call := c.startCall(applyArgs(id, map[string]any{"confirm_code": code})) // the model's code is never used
	formID, params, typed := c.form()
	schema := params["requestedSchema"].(map[string]any)
	field := schema["properties"].(map[string]any)["code"].(map[string]any)
	if code != typed || params["mode"] != nil || !strings.HasPrefix(params["message"].(string), "I'm about to unlock “lock.front”") || field["title"] != "One-time code from HA NOVA" ||
		!strings.HasSuffix(params["message"].(string), "so I need you to confirm it. Nothing has run yet. Type "+code+" to confirm.") || field["type"] != "string" || schema["required"].([]any)[0] != "code" {
		t.Fatalf("form %v (code %s)", params, code)
	}
	if len(h.posts()) != 0 {
		t.Fatal("sent before the human answered")
	}
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	applied := c.final(call)
	if applied["isError"] != false || envelopeOf(applied)["status"] != "verified" || len(h.posts()) != 1 {
		t.Fatalf("accepted: %v (posts %d)", applied, len(h.posts()))
	}
	noCode(t, "applied", applied, code)
}

func TestMCPFormLegacyOutcomes(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	formsInit(c)
	plan := func() (string, string) {
		env := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))
		id := env["plan"].(map[string]any)["id"].(string)
		return id, storedCode(t, h, id)
	}
	id, code := plan()

	for _, tc := range []struct {
		name   string
		answer func(typed string) any
		want   string
	}{
		{"decline", func(string) any { return map[string]any{"action": "decline"} }, "CONFIRM_DECLINED"},
		{"cancel", func(string) any { return map[string]any{"action": "cancel"} }, "CONFIRM_CANCELLED"},
		{"empty code", func(string) any { return map[string]any{"action": "accept", "content": map[string]any{"code": "  "}} }, "CONFIRM_CANCELLED"},
		{"wrong code", func(typed string) any {
			return map[string]any{"action": "accept", "content": map[string]any{"code": wrongCode(typed)}}
		}, "CODE_MISMATCH"},
		{"odd action", func(typed string) any {
			return map[string]any{"action": "maybe", "content": map[string]any{"code": typed}}
		}, "CONFIRM_CANCELLED"},
	} {
		call := c.startCall(applyArgs(id, map[string]any{"confirm_code": code}))
		formID, _, typed := c.form()
		c.answer(formID, tc.answer(typed))
		result := c.final(call)
		step, _ := envelopeOf(result)["next_step"].(string)
		next, _ := envelopeOf(result)["error"].(map[string]any)["next"].(string)
		if result["isError"] != true || errCode(result) != tc.want || len(h.posts()) != 0 || !strings.Contains(step, id) && tc.want != "CONFIRM_DECLINED" ||
			next != step || strings.Contains(step, "in chat") && !strings.Contains(step, "Never ask") {
			t.Fatalf("%s: %v (posts %d)", tc.name, result, len(h.posts()))
		}
		if tc.want != "CODE_MISMATCH" && !strings.Contains(step, "If they saw no confirmation form") { // some apps turn forms down unseen
			t.Fatalf("%s: the step must not claim the user saw a form: %q", tc.name, step)
		}
		noCode(t, tc.name, result, code)
	}

	// A form plan whose text was removed breaks its seal: never applied by
	// the model's code, the plan is gone, and the step is to plan again.
	h.tamper(id, func(p map[string]any) { delete(p, "prompt") })
	if result := c.tool("ha_apply", map[string]any{"plan_id": id, "confirm_code": code}); errCode(result) != "PLAN_INVALID" || len(h.posts()) != 0 ||
		!strings.Contains(envelopeOf(result)["next_step"].(string), "Plan again") {
		t.Fatalf("a plan without form text: %v", result)
	}
	id, code = plan()

	// The client cannot show the form: a JSON-RPC error answer.
	call := c.startCall(applyArgs(id, nil))
	formID, _, _ := c.form()
	c.send(`{"jsonrpc":"2.0","id":"` + formID + `","error":{"code":-32601,"message":"Method not found"}}`)
	if result := c.final(call); errCode(result) != "CONFIRM_UNAVAILABLE" || len(h.posts()) != 0 {
		t.Fatalf("client error: %v", result)
	}

	// No answer in time: the server cancels its own request.
	previous := mcpConfirmWait
	mcpConfirmWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpConfirmWait = previous })
	call = c.startCall(applyArgs(id, nil))
	formID, _, _ = c.form()
	cancelled := c.recvAny()
	if cancelled["method"] != "notifications/cancelled" || cancelled["params"].(map[string]any)["requestId"] != formID {
		t.Fatalf("timeout must cancel the form: %v", cancelled)
	}
	if result := c.final(call); errCode(result) != "CONFIRM_CANCELLED" || len(h.posts()) != 0 {
		t.Fatalf("timeout: %v", result)
	}
	mcpConfirmWait = previous
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": "000000"}}) // too late: never answered
	if pong := c.request("ping", map[string]any{}); pong["result"] == nil || len(h.posts()) != 0 {
		t.Fatalf("a late answer: %v", pong)
	}

	// While a form is open: ping is answered at once, other requests wait, a
	// late answer to an old form is ignored, a waiting request the client
	// cancels never runs, and the client's cancellation of the call ends it
	// without any reply (the server closes its form).
	call = c.startCall(applyArgs(id, nil))
	formID, _, typed := c.form()
	c.send(`{"jsonrpc":"2.0","id":"ha-nova-confirm-1","result":{"action":"accept","content":{"code":"` + typed + `"}}}`)
	c.send(`{"jsonrpc":"2.0","id":"p1","method":"ping"}`)
	if pong := c.recvAny(); pong["id"] != "p1" || pong["result"] == nil {
		t.Fatalf("ping during a form: %v", pong)
	}
	c.send(`{"jsonrpc":"2.0","id":"l1","method":"tools/list"}`)
	c.send(`{"jsonrpc":"2.0","id":"q1","method":"tools/call","params":{"name":"ha_call","arguments":{"service":"light.turn_off","entity_ids":["light.kitchen"]}}}`)
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"q1"}}`)
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":` + jsonNumber(call) + `}}`)
	if closed := c.recvAny(); closed["method"] != "notifications/cancelled" || closed["params"].(map[string]any)["requestId"] != formID {
		t.Fatalf("the cancelled call must close its form: %v", closed)
	}
	if queued := c.recvAny(); queued["id"] != "l1" || queued["result"] == nil {
		t.Fatalf("the queued request must be answered next, the cancelled ones never: %v", queued)
	}
	if pong := c.request("ping", map[string]any{}); pong["result"] == nil || len(h.posts()) != 0 {
		t.Fatalf("a cancelled call or a cancelled waiting request sent (posts %d): %v", len(h.posts()), pong)
	}

	// The right code from the human: exactly one send; the plan is used up.
	call = c.startCall(applyArgs(id, map[string]any{"confirm_code": wrongCode(code)}))
	formID, _, typed = c.form()
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": " " + strings.ToUpper(typed) + " "}})
	applied := c.final(call)
	if applied["isError"] != false || envelopeOf(applied)["status"] != "verified" || len(h.posts()) != 1 {
		t.Fatalf("accepted: %v (posts %d)", applied, len(h.posts()))
	}
	noCode(t, "applied", applied, code)
	if again := c.tool("ha_apply", map[string]any{"plan_id": id}); errCode(again) != "PLAN_NOT_FOUND" || len(h.posts()) != 1 {
		t.Fatalf("a used plan: %v", again)
	}
}

func jsonNumber(n float64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

// 2026-07-28: the form travels as an InputRequiredResult; the client's retry
// carries the answer. The model's code alone never applies, and an accepted
// answer is never replayed.
func TestMCPFormConfirmsTypedPlanModern(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	meta := func(caps map[string]any) map[string]any {
		return map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModernVersion, "io.modelcontextprotocol/clientCapabilities": caps}
	}
	forms := meta(map[string]any{"elicitation": map[string]any{"form": map[string]any{}, "url": map[string]any{}}})
	call := func(params map[string]any) map[string]any {
		params["_meta"] = forms
		msg := c.request("tools/call", params)
		result, _ := msg["result"].(map[string]any)
		if result == nil {
			t.Fatalf("tools/call: %v", msg)
		}
		return result
	}
	planned := call(map[string]any{"name": "ha_call", "arguments": map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}})
	id := envelopeOf(planned)["plan"].(map[string]any)["id"].(string)
	code := storedCode(t, h, id)
	noCode(t, "modern ha_call", planned, code)

	for _, extra := range []map[string]any{nil, {"confirm_code": code}} {
		asked := call(applyArgs(id, extra))
		form := asked["inputRequests"].(map[string]any)["confirm"].(map[string]any)
		message := form["params"].(map[string]any)["message"].(string)
		if asked["resultType"] != "input_required" || asked["content"] != nil || form["method"] != "elicitation/create" || !strings.HasSuffix(message, "Type "+code+" to confirm.") || len(h.posts()) != 0 {
			t.Fatalf("want the form, got %v", asked)
		}
	}
	retry := func(answer map[string]any) map[string]any {
		params := applyArgs(id, map[string]any{"confirm_code": code})
		params["inputResponses"] = map[string]any{"confirm": answer}
		return call(params)
	}
	for _, tc := range []struct {
		answer map[string]any
		want   string
	}{
		{map[string]any{"action": "cancel"}, "CONFIRM_CANCELLED"},
		{map[string]any{"action": "accept"}, "CONFIRM_CANCELLED"},
		{map[string]any{"action": "accept", "content": map[string]any{"code": " \t"}}, "CONFIRM_CANCELLED"},         // nothing typed: no attempt used
		{map[string]any{"action": "accept", "content": map[string]any{"code": code + "."}}, "CONFIRM_CANCELLED"},    // not four digits: can never match, no attempt used
		{map[string]any{"action": "accept", "content": map[string]any{"code": wrongNumber(code)}}, "CODE_MISMATCH"}, // a JSON number reads as its four digits
		{map[string]any{"action": "accept", "content": map[string]any{"code": wrongCode(code)}}, "CODE_MISMATCH"},
	} {
		result := retry(tc.answer)
		step, _ := envelopeOf(result)["next_step"].(string)
		next, _ := envelopeOf(result)["error"].(map[string]any)["next"].(string)
		if errCode(result) != tc.want || next != step || !strings.Contains(step, "plan_id "+id) || len(h.posts()) != 0 {
			t.Fatalf("%v: %v", tc.answer, result)
		}
	}
	if plan, _ := readPlanFile(filepath.Join(planDir(h.paths), id+".json")); plan.Mismatches != 2 {
		t.Fatalf("only the wrong codes count as attempts: %d", plan.Mismatches)
	}
	for _, responses := range []any{map[string]any{"confirm": "x"}, map[string]any{"confirm": map[string]any{"action": 5}}, "x"} {
		params := applyArgs(id, nil)
		params["inputResponses"], params["_meta"] = responses, forms
		if msg := c.request("tools/call", params); errorCode(msg) != rpcInvalidParams || len(h.posts()) != 0 {
			t.Fatalf("malformed inputResponses %v: %v", responses, msg)
		}
	}
	if declined := retry(map[string]any{"action": "decline"}); errCode(declined) != "CONFIRM_DECLINED" || len(h.posts()) != 0 {
		t.Fatalf("declined: %v", declined)
	}
	accept := map[string]any{"action": "accept", "content": map[string]any{"code": " " + code[:2] + " " + code[2:] + " "}} // spaces are ignored
	if applied := retry(accept); envelopeOf(applied)["status"] != "verified" || applied["resultType"] != "complete" || len(h.posts()) != 1 {
		t.Fatalf("accepted: %v (posts %d)", applied, len(h.posts()))
	}
	if replayed := retry(accept); errCode(replayed) != "PLAN_NOT_FOUND" || len(h.posts()) != 1 {
		t.Fatalf("a replayed answer: %v (posts %d)", replayed, len(h.posts()))
	}

	// Without form support (no elicitation, or URL mode only) nothing
	// changes: the code is in the result and confirm_code applies it.
	for _, caps := range []map[string]any{{}, {"elicitation": map[string]any{"url": map[string]any{}}}} {
		forms = meta(caps)
		h.f.states["lock.front"]["state"] = "locked"
		planned := call(map[string]any{"name": "ha_call", "arguments": map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}})
		plan := envelopeOf(planned)["plan"].(map[string]any)
		if say, _ := envelopeOf(planned)["say"].(string); plan["code"] == nil || !strings.Contains(say, plan["code"].(string)) {
			t.Fatalf("%v: the code must stay in the result: %v", caps, planned)
		}
		before := len(h.posts())
		if applied := call(applyArgs(plan["id"].(string), map[string]any{"confirm_code": plan["code"]})); envelopeOf(applied)["status"] != "verified" || len(h.posts()) != before+1 {
			t.Fatalf("%v: confirm_code must apply: %v", caps, applied)
		}
	}
}

// Drift, a config delete, and a failed snapshot: every later result keeps
// the code out, and the form names what it deletes and whether a copy is
// kept.
func TestMCPFormDriftAndDelete(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	h := newWriteHarness(t, f)
	c := startMCP(t, h.paths)
	formsInit(c)

	id := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"].(string)
	h.f.states["lock.front"]["state"] = "jammed"
	call := c.startCall(applyArgs(id, nil))
	formID, _, typed := c.form()
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	drift := c.final(call)
	fresh, _ := envelopeOf(drift)["plan"].(map[string]any)
	step, _ := envelopeOf(drift)["next_step"].(string)
	if say, _ := envelopeOf(drift)["say"].(string); envelopeOf(drift)["status"] != "drift" || len(h.posts()) != 0 || fresh["id"] == id ||
		!strings.Contains(step, "call ha_apply with plan_id "+fresh["id"].(string)) || !strings.HasSuffix(say, sayFormAsk("en")) {
		t.Fatalf("drift: %v", drift)
	}
	noCode(t, "drift", drift, storedCode(t, h, fresh["id"].(string)))
	call = c.startCall(applyArgs(fresh["id"].(string), nil))
	formID, params, _ := c.form()
	if message := params["message"].(string); !strings.HasPrefix(message, "Since the last preview, “lock.front” changed: state was locked, now jammed. I'm about to unlock") {
		t.Fatalf("the form after a drift: %q", message)
	}
	c.answer(formID, map[string]any{"action": "cancel"})
	c.final(call)

	planned := c.tool("ha_plan", map[string]any{"kind": "delete", "entity_id": "automation.night"})
	id = envelopeOf(planned)["plan"].(map[string]any)["id"].(string)
	code := storedCode(t, h, id)
	noCode(t, "ha_plan delete", planned, code)
	h.f.snapshotErr = true
	call = c.startCall(applyArgs(id, nil))
	formID, params, typed = c.form()
	if message := params["message"].(string); !strings.HasPrefix(message, "I'm about to delete the automation “Night”. This removes it from Home Assistant, so I need you to confirm it. Nothing is deleted yet.") || !strings.Contains(message, sayCopyNote("en")) {
		t.Fatalf("delete form: %q", message)
	}
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	failed := c.final(call)
	if step, _ := envelopeOf(failed)["next_step"].(string); errCode(failed) != "SNAPSHOT_FAILED" || len(h.writes()) != 0 ||
		!strings.Contains(step, "plan_id "+id+", no_snapshot true, and no confirm_code") {
		t.Fatalf("snapshot failure: %v", failed)
	}
	noCode(t, "snapshot failure", failed, code)
	call = c.startCall(applyArgs(id, map[string]any{"no_snapshot": true}))
	formID, params, typed = c.form()
	if message := params["message"].(string); strings.Contains(message, sayCopyNote("en")) || !strings.Contains(message, "No copy could be saved") {
		t.Fatalf("no-snapshot form: %q", message)
	}
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	if deleted := c.final(call); envelopeOf(deleted)["status"] != "verified" || len(h.writes()) != 1 {
		t.Fatalf("deleted: %v", deleted)
	}
}

// 2026-07-28: an answer to the form with a copy never confirms the delete
// without one; the form names the item it changes; 2024-11-05 has no forms.
func TestMCPFormBindingAndWording(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	h := newWriteHarness(t, f)
	c := startMCP(t, h.paths)
	meta := map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModernVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{"elicitation": map[string]any{}}}
	call := func(params map[string]any) map[string]any {
		params["_meta"] = meta
		return c.request("tools/call", params)["result"].(map[string]any)
	}
	formMessage := func(asked map[string]any, key string) (string, string) {
		form, _ := asked["inputRequests"].(map[string]any)[key].(map[string]any)
		message, _ := form["params"].(map[string]any)["message"].(string)
		m := formCode.FindStringSubmatch(message)
		if asked["resultType"] != "input_required" || m == nil {
			t.Fatalf("want form %q, got %v", key, asked)
		}
		return message, m[1]
	}
	id := envelopeOf(call(map[string]any{"name": "ha_plan", "arguments": map[string]any{"kind": "delete", "entity_id": "automation.night"}}))["plan"].(map[string]any)["id"].(string)
	_, typed := formMessage(call(applyArgs(id, nil)), "confirm")
	h.f.snapshotErr = true
	answer := map[string]any{"confirm": map[string]any{"action": "accept", "content": map[string]any{"code": typed}}}
	params := applyArgs(id, nil)
	params["inputResponses"] = answer
	if failed := call(params); errCode(failed) != "SNAPSHOT_FAILED" || len(h.writes()) != 0 {
		t.Fatalf("snapshot failure: %v", failed)
	}
	params = applyArgs(id, map[string]any{"no_snapshot": true})
	params["inputResponses"] = answer // the answer to the form that promised a copy
	message, typed := formMessage(call(params), "confirm_without_copy")
	if !strings.Contains(message, "No copy could be saved") || len(h.writes()) != 0 {
		t.Fatalf("no-copy form: %q (writes %v)", message, h.writes())
	}
	params["inputResponses"] = map[string]any{"confirm_without_copy": map[string]any{"action": "accept", "content": map[string]any{"code": typed}}}
	if deleted := call(params); envelopeOf(deleted)["status"] != "verified" || len(h.writes()) != 1 {
		t.Fatalf("deleted without a copy: %v", deleted)
	}

	unlocking := hallDraft("light.turn_on")
	unlocking["alias"], unlocking["actions"] = "Porch light", []any{step("lock.unlock", "lock.front")}
	for args, want := range map[*map[string]any]string{
		{"kind": "write", "domain": "automation", "id": "a1", "draft": unlocking}:     "I'm about to change the automation “Hall motion”. This also unlocks",
		{"kind": "write", "domain": "automation", "create": true, "draft": unlocking}: "I'm about to create the automation “Porch light”. This also unlocks",
	} {
		planned := call(map[string]any{"name": "ha_plan", "arguments": *args})
		id := envelopeOf(planned)["plan"].(map[string]any)["id"].(string)
		if message, _ := formMessage(call(applyArgs(id, nil)), "confirm"); !strings.HasPrefix(message, want) {
			t.Fatalf("form %q, want prefix %q", message, want)
		}
	}

	old := startMCP(t, h.paths)
	old.request("initialize", map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"elicitation": map[string]any{}}})
	planned := envelopeOf(old.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))
	if plan := planned["plan"].(map[string]any); plan["code"] == nil {
		t.Fatalf("2024-11-05 has no elicitation: the code must stay in the result: %v", planned)
	}
}

// Where a form cannot confirm the plan, or none is needed, apply answers at
// once: a preview plan applies, an expired plan or an unoffered no_snapshot is
// refused; stdin closing while a form is open ends the call without a reply.
func TestMCPFormNotShown(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	h := newWriteHarness(t, f)
	c := startMCP(t, h.paths)
	formsInit(c)
	preview := envelopeOf(c.tool("ha_call", map[string]any{"service": "light.turn_off", "area": "kitchen"}))
	if applied := c.tool("ha_apply", map[string]any{"plan_id": preview["plan"].(map[string]any)["id"]}); preview["status"] != "needs_confirmation" || envelopeOf(applied)["status"] != "verified" {
		t.Fatalf("a preview plan needs no form: %v", applied)
	}
	deleteID := envelopeOf(c.tool("ha_plan", map[string]any{"kind": "delete", "entity_id": "automation.night"}))["plan"].(map[string]any)["id"]
	if refused := c.tool("ha_apply", map[string]any{"plan_id": deleteID, "no_snapshot": true}); errCode(refused) != "NO_SNAPSHOT_NOT_OFFERED" || len(h.writes()) != 0 {
		t.Fatalf("no_snapshot before a failed snapshot: %v", refused)
	}
	lockID := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"]
	previous := verbNow
	verbNow = func() time.Time { return previous().Add(planTTL + time.Minute) }
	if expired := c.tool("ha_apply", map[string]any{"plan_id": lockID}); errCode(expired) != "PLAN_EXPIRED" {
		t.Fatalf("an expired plan: %v", expired)
	}
	verbNow = previous

	lockID = envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"]
	before := len(h.posts())
	c.startCall(applyArgs(lockID.(string), nil))
	c.form()
	_ = c.in.Close()
	if line, err := c.out.ReadString('\n'); err == nil || len(h.posts()) != before {
		t.Fatalf("stdin closed during a form: got %q (posts %d)", line, len(h.posts()))
	}
}

// mcp --no-forms: the client's elicitation capability is ignored in both
// eras; the code is in the result and confirm_code applies it.
func TestMCPNoFormsOptOut(t *testing.T) {
	mcpNoForms = true
	t.Cleanup(func() { mcpNoForms = false })
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	formsInit(c)
	plan := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)
	if applied := c.tool("ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": plan["code"]}); plan["code"] == nil || envelopeOf(applied)["status"] != "verified" || len(h.posts()) != 1 {
		t.Fatalf("--no-forms: %v / %v", plan, applied)
	}
	modern := map[string]any{"name": "ha_call", "arguments": map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}},
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": mcpModernVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{"elicitation": map[string]any{}}}}
	h.f.states["lock.front"]["state"] = "locked"
	if planned := envelopeOf(c.request("tools/call", modern)["result"].(map[string]any)); planned["plan"].(map[string]any)["code"] == nil {
		t.Fatalf("--no-forms, 2026-07-28: %v", planned)
	}
}

// B10: a plan made where the client shows forms takes its code only from
// that form. A declined form, a CLI apply with the right code, and a
// --no-forms session's apply all send nothing and use no attempt.
func TestFormPlanRefusesTypedCode(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	formsInit(c)
	id := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"].(string)
	code := storedCode(t, h, id)

	call := c.startCall(applyArgs(id, nil))
	formID, _, _ := c.form()
	c.answer(formID, map[string]any{"action": "decline"})
	if declined := c.final(call); errCode(declined) != "CONFIRM_DECLINED" {
		t.Fatalf("decline: %v", declined)
	}
	for i := 0; i < 4; i++ { // more than the three attempts a code would get
		if exit, env := h.run(runApplyCommand, "--plan", id, "--confirm", code); exit != 4 || env.Error.Code != "CONFIRM_IN_FORM" {
			t.Fatalf("CLI apply with the form plan's code: exit %d %+v", exit, env.Error)
		}
	}
	mcpNoForms = true
	plain := startMCP(t, h.paths)
	formsInit(plain)
	mcpNoForms = false
	if res := plain.tool("ha_apply", map[string]any{"plan_id": id, "confirm_code": code}); errCode(res) != "CONFIRM_IN_FORM" {
		t.Fatalf("--no-forms session: %v", res)
	}
	if len(h.posts()) != 0 {
		t.Fatalf("sent %d requests without the form", len(h.posts()))
	}
	call = c.startCall(applyArgs(id, nil)) // the plan is intact: the human can still confirm in the form
	formID, _, typed := c.form()
	c.answer(formID, map[string]any{"action": "accept", "content": map[string]any{"code": typed}})
	if applied := c.final(call); envelopeOf(applied)["status"] != "verified" || len(h.posts()) != 1 {
		t.Fatalf("form after refusals: %v", applied)
	}
}

// The mismatch counter lives in the user-writable plan file. A form plan
// edited to "chat" would take argv codes against a code nobody saw, and an
// editor resets the counter: the sealed channel refuses that edit.
func TestFormPlanChannelEditRefused(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	c := startMCP(t, h.paths)
	formsInit(c)
	id := envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"].(string)
	file := filepath.Join(planDir(h.paths), id+".json")
	plan, err := readPlanFile(file)
	if err != nil || plan.Channel != channelForm || plan.ChannelSeal == "" {
		t.Fatalf("form plan: %+v %v", plan, err)
	}
	plan.Channel = channelChat
	if err := writePlanFile(file, plan); err != nil {
		t.Fatal(err)
	}
	if exit, env := h.run(runApplyCommand, "--plan", id, "--confirm", storedCode(t, h, id)); exit != 4 || env.Error.Code != "PLAN_INVALID" {
		t.Fatalf("downgraded form plan: exit %d %+v", exit, env.Error)
	}
	// An edited form text would show the human another action next to the
	// real code: no form is shown, and apply refuses the plan.
	h.f.states["lock.front"]["state"] = "locked"
	id = envelopeOf(c.tool("ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}))["plan"].(map[string]any)["id"].(string)
	file = filepath.Join(planDir(h.paths), id+".json")
	if plan, err = readPlanFile(file); err != nil {
		t.Fatal(err)
	}
	plan.Prompt = "Turn on the hallway light?"
	if err := writePlanFile(file, plan); err != nil {
		t.Fatal(err)
	}
	if res := c.tool("ha_apply", map[string]any{"plan_id": id}); errCode(res) != "PLAN_INVALID" {
		t.Fatalf("edited prompt: %v", res)
	}
	if len(h.posts()) != 0 {
		t.Fatalf("sent %d requests for an edited plan", len(h.posts()))
	}
}
