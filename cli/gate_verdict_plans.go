package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// gatePlanTier rates the two core write shapes only a plan's engine can rate
// (Contract → Closed until rated): a service call (computeCallPlan, as plan
// call and apply's recompute run it) and an automation or script config
// write (computeConfigPlan). handled is false for every other request. A
// request the CLI never sends in this shape (checkWriteRequest), or one the
// engine refuses, is tierRefused; a session that doesn't open or a Relay
// that never answers is typed with err.
func gatePlanTier(payload map[string]any, body []byte, session func() (*relaySession, error)) (level tierLevel, why string, handled bool, err error) {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	call := method == http.MethodPost && servicePathPattern.MatchString(path)
	if !call && (method == http.MethodGet || !configPathPattern.MatchString(path)) {
		return 0, "", false, nil
	}
	if checkWriteRequest("core", payload) != nil {
		return tierRefused, "", true, nil
	}
	if method == http.MethodDelete {
		return tierTyped, reasonConfigDelete, true, nil
	}
	var req planCallRequest
	if call {
		var ok bool
		if req, ok = gateCallRequest(path, payload["body"]); !ok {
			return tierRefused, "", true, nil
		}
	}
	s, err := session()
	if err != nil {
		return tierTyped, "", true, err
	}
	var e *tierEngine
	env := verbEnvelope{SchemaVersion: 1, Verb: "gate"}
	if call {
		e, _, err = computeCallPlan(s, &env, req)
	} else {
		var core struct {
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(body, &core) // gateVerdict decoded it already
		domain, id, _ := strings.Cut(strings.TrimPrefix(path, "/api/config/"), "/config/")
		e, _, _, err = computeConfigPlan(runtimePaths{}, s, &env, configRequest{Op: "write", Domain: domain, ConfigID: id, Draft: core.Body})
	}
	var failure *verbFailure
	switch {
	case errors.As(err, &failure):
		return tierRefused, failure.code, true, nil
	case err != nil:
		return tierTyped, "", true, err
	}
	if len(e.reasons) > 0 {
		why = e.reasons[0].Code
	}
	return e.level, why, true, nil
}

// gateCallRequest turns the body executeAction sends (the planned data plus
// the frozen entity_id list) back into the request a plan rates. The skill
// is the service's owner, as the CLI sends it only from there.
func gateCallRequest(path string, body any) (planCallRequest, bool) {
	data, ok := body.(map[string]any)
	if !ok {
		return planCallRequest{}, false
	}
	route, response := strings.CutSuffix(strings.TrimPrefix(path, "/api/services/"), "?return_response")
	domain, name, _ := strings.Cut(route, "/")
	req := planCallRequest{Service: domain + "." + name, Data: map[string]any{}, Response: response, Skill: ownerSkill(domain + "." + name)}
	if domain == "hassio" {
		req.Skill = "ha-nova:service-call"
	}
	for key, value := range data {
		if key != "entity_id" {
			req.Data[key] = value
			continue
		}
		ids, isList := value.([]any)
		for _, id := range ids {
			text, isText := id.(string)
			if !isText {
				return planCallRequest{}, false
			}
			req.EntityIDs = append(req.EntityIDs, text)
		}
		if !isList || len(ids) == 0 {
			return planCallRequest{}, false
		}
	}
	return req, req.validate() == nil && staticRefusal(req) == nil
}
