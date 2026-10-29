package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Applying a raw plan: the items in order, each checked by the raw guard
// right before it is sent (an earlier item may have renamed an entity id
// or finished a flow), never retried, stopping at the first one that did
// not go through. Every response is redacted like `relay`'s. Leading
// backups saves are the config snapshot (skills/ha-nova/config-snapshots.md):
// when one fails, nothing else is sent and the plan stays valid for
// --no-snapshot. A helper update gets its update-revert checkpoint from a
// read before and after (skills/ha-nova/update-revert.md → Manual capture).

// rawItemOutcome is one item after apply: done (Home Assistant accepted
// it), rejected (it refused), unknown (it may have run), refused (the guard
// stopped it, unsent), not_sent, or skipped (a snapshot left out by
// --no-snapshot).
type rawItemOutcome struct {
	Item     int                  `json:"item"`
	Endpoint string               `json:"endpoint"`
	Status   string               `json:"status"`
	Response json.RawMessage      `json:"response,omitempty"`
	Error    string               `json:"error,omitempty"`
	Undo     *snapshotSaveReceipt `json:"undo,omitempty"`
}

type rawApplyResult struct {
	Items []rawItemOutcome `json:"items"`
}

// rawHelperCollections: the helper storage family update-revert covers.
var rawHelperCollections = []string{"input_boolean", "input_number", "input_text", "input_select", "input_datetime",
	"input_button", "counter", "timer", "schedule"}

func executeRaw(paths runtimePaths, s *relaySession, env *verbEnvelope, items []rawRequest, noSnapshot bool) (err error) {
	sent := false
	defer afterSend(&sent, env, &err)
	result, copies := rawApplyResult{}, len(rawCopies(items)) // steps are numbered and counted after the copies, as the preview says them
	step := func(i int) int { return i + 1 - copies }
	add := func(i int, status, why string) *rawItemOutcome {
		result.Items = append(result.Items, rawItemOutcome{Item: i + 1, Endpoint: items[i].Endpoint, Status: status, Error: why})
		return &result.Items[len(result.Items)-1]
	}
	stop := func(i int, status, why string) error {
		add(i, status, why)
		for j := i + 1; j < len(items); j++ {
			add(j, "not_sent", "")
		}
		return rawStopped(env, result, copies, i, status, why)
	}
	undo := func(i int) error {
		return &verbFailure{exit: verbExitError, code: "UNDO_FAILED", cause: errSnapshotFailed,
			message: fmt.Sprintf("step %d's helper could not be read before its change, so no undo could be saved; nothing was sent and the plan stays valid", step(i)),
			next:    "Tell the user the change could not be made undoable. Only if they want it anyway: apply again with --no-snapshot (no config copy, no undo)."}
	}
	for i, item := range items { // every helper's undo before anything is sent
		payload, _ := decodeObject(item.Payload)
		if collection, id := rawHelperUpdate(item.Endpoint, payload); !noSnapshot && collection != "" && rawHelperItem(s, collection, id) == nil {
			return undo(i)
		}
	}
	snapshotting, withoutUndo := true, false // still in the leading backups saves; a helper changed without undo
	for i, item := range items {
		payload, _ := decodeObject(item.Payload)
		snapshot := snapshotting && item.Endpoint == "backups" && payload["action"] == "save"
		snapshotting = snapshot
		if snapshot && noSnapshot {
			add(i, "skipped", "the user chose to go on without a config snapshot")
			continue
		}
		s.renewBudget() // 20 items may outlast one budget
		body, refusal, gerr := guardRawRequestIn(s, item.Endpoint, item.Payload)
		switch {
		case refusal != nil && !sent:
			return &verbFailure{exit: verbExitRefused, code: "REFUSED", message: fmt.Sprintf("item %d: %s", i+1, refusal.Error()),
				next: "Something changed since the preview. Tell the user; plan again only if they still want it."}
		case gerr != nil && !sent:
			return nothingSent(gerr)
		case refusal != nil:
			return stop(i, "refused", refusal.Error())
		case gerr != nil:
			return stop(i, "not_sent", gerr.Error())
		}
		collection, id := rawHelperUpdate(item.Endpoint, payload)
		before := rawHelperItem(s, collection, id)
		switch {
		case collection == "" || before != nil:
		case noSnapshot:
			withoutUndo = true
			env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: fmt.Sprintf("step %d is not revertible: the user chose to go on without undo", step(i))})
		case !sent:
			return undo(i)
		default:
			return stop(i, "not_sent", "its helper could not be read before the change, so no undo could be saved")
		}
		sent = true
		data, werr := s.sendWrite(item.Endpoint, body)
		status, why := rawSent(item.Endpoint, body, data, werr)
		if status != "done" && snapshot {
			return &verbFailure{exit: verbExitError, code: "SNAPSHOT_FAILED",
				message: fmt.Sprintf("no config snapshot could be saved (%s); nothing else was sent and the plan stays valid", why),
				next:    "Tell the user there is no copy. They choose: prune the snapshot store, take a full backup (ha-nova:backup), or go on without a copy: then apply again with --no-snapshot.",
				cause:   errSnapshotFailed}
		}
		if status == "done" && item.Endpoint == "backups" && payload["action"] == "delete" {
			status, why = rawSnapshotGone(s, payload)
		}
		if status != "done" {
			return stop(i, status, why)
		}
		out := add(i, "done", "")
		out.Response = rawResponse(item.Endpoint, data)
		if before != nil {
			if out.Undo = saveHelperCheckpoint(paths, s, env, collection, id, before, payload); out.Undo != nil {
				env.Undo = out.Undo
			}
		}
	}
	env.Result, env.Status, env.exit = result, "accepted", verbExitOK
	env.Say = "Done: Home Assistant accepted it."
	if done := countStatus(rawApplyResult{Items: result.Items[copies:]}, "done"); done > 1 {
		env.Say = fmt.Sprintf("Done: Home Assistant accepted all %d steps.", done)
	}
	if countStatus(result, "skipped") > 0 || withoutUndo {
		env.Say += " No copy or undo was saved, as you chose."
	}
	env.NextStep = "Tell the user what each item's response says where it matters (result.items[].response: a setup flow's next form, a created id). A further step is a new plan."
	return nil
}

func countStatus(result rawApplyResult, status string) int {
	n := 0
	for _, item := range result.Items {
		if item.Status == status {
			n++
		}
	}
	return n
}

// rawSnapshotGone: after a config copy's delete, the store must no longer
// list it before the next step runs (skills/ha-nova/config-snapshots.md →
// Batch delete).
func rawSnapshotGone(s *relaySession, payload map[string]any) (status, why string) {
	file, _ := payload["file"].(string)
	data, err := rawSnapshotList(s)
	var rows []struct {
		File string `json:"file"`
	}
	if err == nil {
		err = json.Unmarshal(data, &rows)
	}
	if err != nil {
		return "unknown", "the copies could not be listed again to check the delete: " + err.Error()
	}
	for _, row := range rows {
		if row.File == file {
			return "failed", "the copy is still listed after its delete"
		}
	}
	return "done", ""
}

// rawSent classifies one sent item. A request that only reads changed
// nothing, so its failure is never unknown.
func rawSent(endpoint string, body []byte, data json.RawMessage, err error) (status, why string) {
	var unknown *relayOutcomeUnknownError
	var call *relayCallError
	read := rawRelayRead(endpoint, body)
	switch {
	case err != nil && errors.As(err, &unknown) && !read:
		return "unknown", err.Error()
	case err != nil && errors.As(err, &call) && call.code == "SUPERVISOR_OUTCOME_UNKNOWN": // it may still finish
		return "unknown", mcpMessage(err.Error(), mcpMessageMax)
	case err != nil && (errors.As(err, &call) || read):
		return "rejected", mcpMessage(err.Error(), mcpMessageMax)
	case err != nil: // it never reached Home Assistant
		return "not_sent", err.Error()
	case endpoint != "core" && endpoint != "supervisor":
		return "done", ""
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(data, &core) != nil || core.Status == 0 {
		if read {
			return "rejected", "the Relay sent no upstream status"
		}
		return "unknown", "the Relay sent no upstream status"
	}
	answer := fmt.Sprintf("Home Assistant answered HTTP %d", core.Status)
	if endpoint == "supervisor" {
		answer = fmt.Sprintf("the Supervisor answered HTTP %d", core.Status)
	}
	if reason := upstreamReason(redactRawResponse(endpoint, core.Body)); reason != "" {
		answer += ": " + reason
	}
	switch {
	case core.Status >= 500 && !read:
		return "unknown", answer
	case core.Status >= 400:
		return "rejected", answer
	}
	return "done", ""
}

// rawResponse: the item's answer as `relay` shows it (redacted), or a note
// when it would fill the chat.
func rawResponse(endpoint string, data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	data = redactRawResponse(endpoint, data)
	if len(data) > mcpReadMaxResult {
		note, _ := json.Marshal(fmt.Sprintf("a response of %d bytes, not shown; read the result with ha-nova relay %s (ha_read in chat apps)", len(data), endpoint))
		return note
	}
	return data
}

// rawStopped records an apply that stopped at item i (0-based) after
// earlier items were done; the result keeps every item's outcome. Its text
// numbers and counts the steps after the leading copies, as the preview did.
func rawStopped(env *verbEnvelope, result rawApplyResult, copies, i int, status, why string) error {
	done, step := countStatus(rawApplyResult{Items: result.Items[copies:]}, "done"), i+1-copies
	went, rest, sayRest := "", "", ""
	if done > 0 {
		went = "; " + plural(done, "earlier step") + " went through"
	}
	if len(result.Items) > i+1 {
		rest, sayRest = "; the steps after it were not sent", ", and the rest weren't sent"
	}
	env.Result, env.Status, env.exit = result, "failed", verbExitUnverified
	env.Error = &verbError{Code: "RAW_FAILED", Message: fmt.Sprintf("step %d did not go through (%s)%s%s", step, why, went, rest),
		Next: "Tell the user which steps went through and what stopped the next one. Plan the rest again only if they still want it."}
	switch {
	case status == "unknown":
		env.Status, env.exit, env.OutcomeUnknown = "unverified", verbExitUnknown, true
		env.Error.Code = "OUTCOME_UNKNOWN"
		env.Error.Message = fmt.Sprintf("step %d may have reached Home Assistant (%s)%s%s", step, why, went, rest)
		env.Error.Next = "Read whether that step took effect and tell the user; never apply this plan again. The steps after it were not sent."
		env.Say = fmt.Sprintf("Not sure: step %d may have gone through, and I can't tell. I didn't try again%s.", step, sayRest)
	case done == 0 && status == "rejected":
		env.Say = sayNotDone("Home Assistant refused it")
	case done == 0:
		env.Say = sayNotDone("it didn't go through")
	default:
		env.Say = fmt.Sprintf("Partly done: %s went through; step %d didn't%s.", plural(done, "step"), step, sayRest)
	}
	return nil
}

// rawHelperUpdate: the helper collection and item id of a helper update.
func rawHelperUpdate(endpoint string, payload map[string]any) (collection, id string) {
	wsType, _ := payload["type"].(string)
	collection, op, idKey := rawCollection(wsType)
	id, _ = payload[idKey].(string)
	if endpoint != "ws" || op != "update" || id == "" || !containsText(rawHelperCollections, collection) {
		return "", ""
	}
	return collection, id
}

// rawHelperItem reads one helper from its collection's list, live; nil
// when it can't be read (the update is then not revertible).
func rawHelperItem(s *relaySession, collection, id string) map[string]any {
	if collection == "" {
		return nil
	}
	data, err := rawRead(s, "ws", map[string]any{"type": collection + "/list"})
	var rows []map[string]any
	if err != nil || json.Unmarshal(data, &rows) != nil {
		return nil
	}
	for _, row := range rows {
		if row["id"] == id {
			return row
		}
	}
	return nil
}

// saveHelperCheckpoint saves the update-revert record of a helper update
// once the read-back shows every field it set; otherwise a notice says it
// is not revertible.
func saveHelperCheckpoint(paths runtimePaths, s *relaySession, env *verbEnvelope, collection, id string, before, payload map[string]any) *snapshotSaveReceipt {
	after := rawHelperItem(s, collection, id)
	_, _, idKey := rawCollection(collection + "/update")
	verified := after != nil
	for key, value := range payload {
		if key != "type" && key != idKey && verified {
			verified = jsonEqual(value, after[key])
		}
	}
	notice := func(why string) *snapshotSaveReceipt {
		env.Notices = append(env.Notices, verbNotice{Kind: "undo", Text: fmt.Sprintf("the %s update of %s is not revertible: %s", collection, id, why)})
		return nil
	}
	if !verified {
		return notice("its read-back does not show the update")
	}
	name, _ := after["name"].(string)
	record, _ := json.Marshal(map[string]any{"op": "update", "domain": collection, "target_id": id, "name": strings.TrimSpace(name),
		"server": s.server, "before_config": before, "expected_after": after})
	receipt, err := saveUndoSnapshotBytes(paths, record)
	if err != nil {
		return notice(err.Error())
	}
	return &receipt
}
