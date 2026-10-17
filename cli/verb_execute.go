package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Home Assistant statuses that prove the call was rejected before running.
var rejectedStatuses = map[int]bool{400: true, 404: true, 422: true}

// executeAction sends the frozen action exactly once — the payload is the
// frozen entity_ids plus the planned data, nothing else (I6, I7) — then
// verifies it on a fresh read budget.
func executeAction(s *relaySession, e *tierEngine, env *verbEnvelope, action planAction) (err error) {
	sent := false
	defer afterSend(&sent, env, &err)
	body := map[string]any{}
	for key, value := range action.Data {
		body[key] = value
	}
	if len(action.EntityIDs) > 0 {
		body["entity_id"] = slices.Clone(action.EntityIDs)
	}
	s.renewBudget()
	flowsBefore := s.reauthFlows() // best effort, bounded; nil means no correlation
	sent = true
	result, err := s.postService(action.Service, body, action.Response)
	s.renewBudget() // verification and correlation get their own budget
	var unknown *relayOutcomeUnknownError
	switch {
	case errors.As(err, &unknown):
		env.OutcomeUnknown = true
		return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: err.Error(), next: "Verify the target state; never retry automatically."}
	case err != nil: // refused or answered before reaching Home Assistant
		return nothingSent(err)
	case rejectedStatuses[result.status]:
		return &verbFailure{exit: verbExitUnverified, status: "failed", code: "SERVICE_REJECTED",
			message: fmt.Sprintf("Home Assistant rejected the call (HTTP %d): %s", result.status, truncate(string(result.body), 300)),
			next:    "Nothing ran; check the service data and the target."}
	}
	if action.Response && result.status < 400 && len(result.body) > 0 {
		// The whole upstream body, so the skills' `result.service_response[...]`
		// path matches what the old raw `.data.body.service_response` gave.
		env.Result = json.RawMessage(result.body)
	}
	rows, outcome := verifyCall(s, e, action)
	env.Verification = rows
	if result.status >= 400 { // e.g. 500: it may have partly run — probes shown, never verified
		if result.status >= 500 && flowsBefore != nil {
			env.ReauthOpened = newReauthFlows(s, e, action.EntityIDs, flowsBefore)
		}
		return &verbFailure{exit: verbExitUnverified, status: "unverified", code: "SERVICE_ERROR",
			message: fmt.Sprintf("Home Assistant answered HTTP %d: %s", result.status, truncate(string(result.body), 300)),
			next:    "Do not retry automatically; check the target and the integration."}
	}
	env.Status, env.exit = outcome, verbExitOK
	if outcome == "unverified" || outcome == "failed" {
		env.exit = verbExitUnverified
	}
	return nil
}

// newReauthFlows reports reauth flows that opened during the call and belong
// to a target: same config entry, or same integration with exactly one
// entry. This is the rule's source since the service-call skill moved to
// `ha-nova call` (formerly its Generic 500 with a reauth side effect section).
func newReauthFlows(s *relaySession, e *tierEngine, targets []string, before map[string]reauthFlow) []string {
	time.Sleep(reauthSettle)
	rows, _ := e.fullRegistry()
	var entries []struct {
		EntryID string `json:"entry_id"`
		Domain  string `json:"domain"`
	}
	_ = s.wsDecode(map[string]any{"type": "config_entries/get"}, &entries, "the config entries")
	perDomain := map[string]int{}
	for _, entry := range entries {
		perDomain[entry.Domain]++
	}
	targetEntries, targetPlatforms := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		if slices.Contains(targets, row.EntityID) {
			targetEntries[row.ConfigEntryID], targetPlatforms[row.Platform] = true, true
		}
	}
	var opened []string
	for id, flow := range s.reauthFlows() {
		_, existed := before[id]
		matches := (flow.entryID != "" && targetEntries[flow.entryID]) || (targetPlatforms[flow.handler] && perDomain[flow.handler] == 1)
		if !existed && matches {
			opened = append(opened, flow.handler)
		}
	}
	slices.Sort(opened)
	return opened
}

func truncate(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return text[:n] + "…"
}
