package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The human confirms, not the model (docs/work/2026-10-16-mcp-human-
// confirmation-spec.md). With a client that shows confirmation forms (MCP
// elicitation, form mode) a typed plan's code is never in a tool result, and
// ha_apply asks the human for it in a form; a confirm_code from the model is
// never used there. Apply itself is unchanged: claim, recompute, drift, the
// code from the recomputed plan, one send.

// mcpConfirmWait bounds an open form in the legacy era; the plan's expiry
// bounds it too.
var mcpConfirmWait = 5 * time.Minute

// formElicitation: the client's elicitation capability covers form mode.
// {} means form in every revision that has elicitation; {url:{}} alone does
// not.
func formElicitation(capabilities map[string]any) bool {
	modes, declared := capabilities["elicitation"].(map[string]any)
	_, form := modes["form"]
	_, url := modes["url"]
	return declared && (form || !url)
}

// withholdCode: where the client shows confirmation forms, a typed plan's
// code never reaches the model; the human reads it in the form instead.
func withholdCode(envelope map[string]any) {
	plan, _ := envelope["plan"].(map[string]any)
	code, _ := plan["code"].(string)
	if code == "" {
		return
	}
	delete(plan, "code")
	if say, _ := envelope["say"].(string); say != "" {
		envelope["say"] = withholdAsk(say, code)
	}
}

// formsNext withholds the code and returns next_step where the client shows
// confirmation forms; an error's own next says the same (one instruction per
// result).
func formsNext(envelope map[string]any, step string) string {
	withholdCode(envelope)
	next := formsStep(envelope, step)
	if errInfo, _ := envelope["error"].(map[string]any); errInfo != nil && next != step {
		errInfo["next"] = next
	}
	return next
}

// formsStep: a typed plan goes to ha_apply, whose form asks the human; a
// wrong or missing code shows the form again or asks for a new plan.
func formsStep(envelope map[string]any, step string) string {
	plan, _ := envelope["plan"].(map[string]any)
	errInfo, _ := envelope["error"].(map[string]any)
	say, _ := envelope["say"].(string)
	pending := envelope["status"] == "needs_confirmation" || envelope["status"] == "drift"
	switch {
	case pending && plan["confirm"] == "code" && strings.Contains(say, sayFormAsk):
		return fmt.Sprintf("Show this preview. When the user has asked for this change and your workflow has reached this step (a skill's order comes first), call ha_apply with plan_id %v and no confirm_code: their app asks them to type the one-time code from HA NOVA in a confirmation form you never see. Never ask for it in chat. Tell the user `say` as the confirmation text (in their language; names and state values exactly as printed) and add no other reason.", plan["id"])
	case errInfo["code"] == "CODE_MISMATCH":
		return fmt.Sprintf("The one-time code typed in the form did not match; nothing was sent. If the user still wants this, call ha_apply again with plan_id %v to show the form again.", plan["id"])
	case errInfo["code"] == "CODE_REQUIRED": // a typed plan without form text (made by an older HA NOVA)
		return "This plan can't be confirmed in a form here; plan it again, show the new preview, and confirm again."
	case errInfo["code"] == "SNAPSHOT_FAILED":
		return step + fmt.Sprintf(" To delete without a copy (only if the user chooses that), call ha_apply with plan_id %v, no_snapshot true, and no confirm_code: their app asks them for the one-time code again in a form.", plan["id"])
	}
	return step
}

// elicitAnswer is the client's ElicitResult; stop marks an outcome without
// one (timeout, a form the client could not show).
type elicitAnswer struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
	stop    string
}

// rpcNoReply ends a request without a response: the client cancelled it or
// is gone.
var rpcNoReply = &rpcError{Message: "no reply"}

// confirmByForm runs before ha_apply where the request has form
// elicitation: the model's confirm_code is dropped, and what the human
// typed in the form takes its place. It returns a result
// when the call ends here: the form to show (2026-07-28), or why nothing was
// sent. A plan no form can confirm goes to apply without a code, which
// refuses it exactly as the CLI does.
func (srv *mcpServer) confirmByForm(req rpcRequest, args map[string]json.RawMessage) (map[string]any, *rpcError) {
	delete(args, "confirm_code")
	var retry struct {
		InputResponses json.RawMessage `json:"inputResponses"` // 2026-07-28: the retry carries the form's answer
	}
	var responses map[string]json.RawMessage
	if json.Unmarshal(req.Params, &retry) != nil || retry.InputResponses != nil && json.Unmarshal(retry.InputResponses, &responses) != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "inputResponses must be an object"}
	}
	plan, noSnapshot, ok := srv.formPlan(args)
	if !ok {
		return nil, nil
	}
	params := confirmForm(plan, noSnapshot)
	key := "confirm" // an answer to the form with a copy never confirms the one without
	if noSnapshot {
		key = "confirm_without_copy"
	}
	var answer elicitAnswer
	if req.modern {
		raw, answered := responses[key]
		if !answered {
			return map[string]any{"resultType": "input_required", "inputRequests": map[string]any{
				key: map[string]any{"method": "elicitation/create", "params": params}}}, nil
		}
		if json.Unmarshal(raw, &answer) != nil || !slices.Contains([]string{"accept", "decline", "cancel"}, answer.Action) {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "inputResponses." + key + " must be an ElicitResult (action accept, decline, or cancel)"}
		}
	} else {
		expires, _ := time.Parse(time.RFC3339, plan.ExpiresAt)
		var rpcErr *rpcError
		if answer, rpcErr = srv.elicit(req.ID, params, min(mcpConfirmWait, expires.Sub(verbNow()))); rpcErr != nil {
			return nil, rpcErr
		}
	}
	typed, _ := answer.Content["code"].(string)
	typed = typedCode(typed) // anything but the four digits is a wrong code, as in the CLI
	// Some apps declare forms but turn them down or close them without showing
	// one: the wording never claims that the user saw it.
	elsewhere := "If they saw no confirmation form, their app can't show one here: they can make this change in Home Assistant itself or with HA NOVA in a terminal, or add \"--no-forms\" to the ha-nova MCP entry's args (the model then sees the one-time code too)."
	again := fmt.Sprintf("Tell the user nothing changed. %s Otherwise call ha_apply again with plan_id %s (it shows the form again) only after the user says they still want it.", elsewhere, plan.ID)
	switch {
	case answer.stop == "" && answer.Action == "accept" && typed != "":
		args["confirm_code"], _ = json.Marshal(typed)
		return nil, nil
	case answer.stop == "" && answer.Action == "decline":
		return confirmStopped("declined", "CONFIRM_DECLINED", "the confirmation form was declined",
			"Tell the user nothing changed. "+elsewhere+" Apply this plan again only if they ask for it."), nil
	case answer.stop == "unavailable":
		return confirmStopped("error", "CONFIRM_UNAVAILABLE", "the app could not show the confirmation form",
			"Tell the user this change needs their confirmation in a form this app could not show; they can make it in Home Assistant itself, or with HA NOVA in a terminal."), nil
	case answer.stop == "timeout":
		return confirmStopped("cancelled", "CONFIRM_CANCELLED", "the confirmation form got no answer in time", again), nil
	}
	return confirmStopped("cancelled", "CONFIRM_CANCELLED", "the confirmation form was closed without the one-time code", again), nil
}

// formPlan reads the plan ha_apply names when a form can confirm it: typed,
// with its form text, and not refused by apply before the code check
// (expired, another server's, an old-format code, --no-snapshot not offered). The file only
// decides whether to ask; apply re-checks everything.
func (srv *mcpServer) formPlan(args map[string]json.RawMessage) (storedPlan, bool, bool) {
	id := text(args, "plan_id")
	var noSnapshot bool
	_ = json.Unmarshal(args["no_snapshot"], &noSnapshot)
	if !planIDPattern.MatchString(id) {
		return storedPlan{}, false, false
	}
	plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json"))
	expires, timeErr := time.Parse(time.RFC3339, plan.ExpiresAt)
	ok := err == nil && timeErr == nil && plan.ID == id && plan.Tier == tierTyped.String() && plan.Prompt != "" && !staleCode(plan) &&
		verbNow().Before(expires) && (srv.pin == "" || srv.pin == plan.Server) &&
		(!noSnapshot || plan.Kind == "delete" && plan.SnapshotFailed)
	return plan, noSnapshot, ok
}

// confirmForm is the elicitation/create params: the plan's code-free text,
// the copy note of a delete, and the four-digit code to type, derived like
// apply does. No mode field: form is the default in every revision.
func confirmForm(plan storedPlan, noSnapshot bool) map[string]any {
	code := confirmCode(planIdentity(strings.TrimPrefix(plan.Hash, "sha256:"), plan.Nonce))
	message := plan.Prompt
	if plan.Kind == "delete" {
		note := sayCopyNote
		if noSnapshot {
			note = "No copy could be saved, so once deleted it can't be restored."
		}
		message += " " + note
	}
	message += " To go ahead, type this one-time code from HA NOVA: " + code
	field := map[string]any{"type": "string", "title": "One-time code from HA NOVA", "description": "Type " + code + " to go ahead", "minLength": 4, "maxLength": 12}
	return map[string]any{"message": message, "requestedSchema": map[string]any{"type": "object",
		"properties": map[string]any{"code": field}, "required": []string{"code"}}}
}

// confirmStopped: nothing was sent and the plan is untouched (no claim
// precedes the answer).
func confirmStopped(status, code, message, next string) map[string]any {
	env := verbEnvelope{SchemaVersion: 1, Verb: "apply"}
	setVerbError(&env, &verbFailure{exit: verbExitRefused, status: status, code: code, message: message + "; nothing was sent", next: next})
	out, _ := json.Marshal(env)
	return toolResult("ha_apply", string(out), "", verbExitRefused, nil, true)
}

// elicit sends elicitation/create inside the running tools/call (legacy
// era) and waits for the answer. Meanwhile ping is answered and every other
// message waits in the queue; a cancelled call or a closed stdin ends it
// without a reply; on timeout the server cancels its own request.
func (srv *mcpServer) elicit(callID json.RawMessage, params map[string]any, wait time.Duration) (elicitAnswer, *rpcError) {
	srv.formSeq++
	id := fmt.Sprintf("ha-nova-confirm-%d", srv.formSeq)
	srv.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": "elicitation/create", "params": params})
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			srv.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id, "reason": "no answer in time"}})
			return elicitAnswer{stop: "timeout"}, nil
		case line, open := <-srv.lines:
			if !open {
				return elicitAnswer{}, rpcNoReply
			}
			var msg struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params struct {
					RequestID json.RawMessage `json:"requestId"`
				} `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			var answerID string
			response := json.Unmarshal(line, &msg) == nil && msg.Method == "" && (msg.Result != nil || msg.Error != nil)
			switch {
			case response && json.Unmarshal(msg.ID, &answerID) == nil && answerID == id:
				var answer elicitAnswer
				if msg.Error != nil && string(msg.Error) != "null" || json.Unmarshal(msg.Result, &answer) != nil {
					return elicitAnswer{stop: "unavailable"}, nil
				}
				return answer, nil
			case response: // a late answer to an earlier form: nothing waits for it
			case msg.Method == "notifications/cancelled" && sameID(msg.Params.RequestID, callID):
				srv.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": id, "reason": "the call was cancelled"}})
				return elicitAnswer{}, rpcNoReply
			case msg.Method == "notifications/cancelled":
				srv.dropQueued(msg.Params.RequestID) // a waiting request the client gave up on never runs
			case msg.Method == "ping" && len(msg.ID) > 0:
				srv.reply(msg.ID, map[string]any{}, nil)
			default:
				srv.queued = append(srv.queued, line) // the main loop answers it, invalid ones included
			}
		}
	}
}

// dropQueued removes the queued request with this id.
func (srv *mcpServer) dropQueued(id json.RawMessage) {
	kept := srv.queued[:0]
	for _, line := range srv.queued {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.Method == "" || !sameID(msg.ID, id) {
			kept = append(kept, line)
		}
	}
	srv.queued = kept
}

// sameID compares two JSON-RPC ids (strings or numbers) by value.
func sameID(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	switch x.(type) {
	case string, float64:
		return x == y // a y of another dynamic type compares unequal, never panics
	}
	return false
}
