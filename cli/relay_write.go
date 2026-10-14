package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// The single write surface of the typed verbs (spec §2.3, I7): exactly one
// POST /api/services/<domain>/<service> per execution, never retried, never
// through the read path (read() refuses it, I1). Nothing else writes.

var relayWriteTimeout = 30 * time.Second

// serviceResult is Home Assistant's own answer to the POST.
type serviceResult struct {
	status int
	body   json.RawMessage
}

// postService sends one service call. Errors are either a relayCallError /
// plain error (nothing reached Home Assistant: exit 1) or a
// relayOutcomeUnknownError (it may have run: exit 7, never retry).
func (s *relaySession) postService(service string, body map[string]any) (serviceResult, error) {
	domain, name := splitID(service)
	if !entityIDPattern.MatchString(service) {
		return serviceResult{}, fmt.Errorf("%q is not a domain.service; nothing was sent", service)
	}
	payload, err := json.Marshal(map[string]any{"method": http.MethodPost, "path": "/api/services/" + domain + "/" + name, "body": body})
	if err != nil {
		return serviceResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
	defer cancel()
	req, err := newRelayRequest(ctx, s.transport, "core", payload)
	if err != nil {
		return serviceResult{}, err
	}
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return serviceResult{}, relayRequestOutcomeUnknownError(s.transport.BaseURL, err)
	}
	defer resp.Body.Close()
	if isHTTPRedirect(resp.StatusCode) {
		return serviceResult{}, relayHTTPOutcomeUnknownError(resp.StatusCode)
	}
	if s.transport.Via == relayViaLocal && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		return serviceResult{}, errors.New(localRelayAuthRepairMessage(resp.StatusCode, s.server, s.cfg.RelayBaseURL))
	}
	raw, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err == nil {
		raw, err = normalizeUTF8Bytes(raw, "Relay response")
	}
	if err != nil {
		return serviceResult{}, relayPostRequestOutcomeUnknownError("reading the Relay response", err)
	}
	var envelope struct {
		OK   *bool `json:"ok"`
		Data struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.OK == nil {
		return serviceResult{}, relayPostRequestOutcomeUnknownError("validating the Relay result envelope", errors.New("not a valid result envelope"))
	}
	if !*envelope.OK {
		if resp.StatusCode >= http.StatusInternalServerError { // upstream error or timeout: it may have run
			return serviceResult{}, relayHTTPOutcomeUnknownError(resp.StatusCode)
		}
		return serviceResult{}, &relayCallError{endpoint: "core", status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	if envelope.Data.Status == 0 {
		return serviceResult{}, relayPostRequestOutcomeUnknownError("reading the upstream status", errors.New("no upstream status"))
	}
	return serviceResult{status: envelope.Data.Status, body: envelope.Data.Body}, nil
}

// readFresh bypasses the memo — verification polls and the reauth re-read
// must observe the state after the write — under the same allowlist (I1).
func (s *relaySession) readFresh(endpoint string, payload map[string]any) (json.RawMessage, error) {
	if err := checkReadOnlyRequest(endpoint, payload); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return s.send(endpoint, body)
}

func (s *relaySession) stateFresh(entityID string) (*entityState, error) {
	data, err := s.readFresh("core", map[string]any{"method": http.MethodGet, "path": "/api/states/" + entityID})
	if err != nil {
		return nil, err
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &core); err != nil || core.Status == 0 {
		return nil, fmt.Errorf("Relay core response for %s has no upstream status", entityID)
	}
	if core.Status == http.StatusNotFound {
		return nil, nil
	}
	if core.Status >= 400 {
		return nil, fmt.Errorf("Home Assistant answered HTTP %d for %s", core.Status, entityID)
	}
	st := &entityState{raw: core.Body}
	return st, json.Unmarshal(core.Body, st)
}

// renewBudget opens a fresh overall read budget for the phase after
// planning. Earlier contexts stay alive until close (a transport may hold one).
func (s *relaySession) renewBudget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.cancel
	ctx, cancel := context.WithTimeout(context.Background(), relaySessionOverallTimeout)
	s.ctx, s.cancel = ctx, func() { cancel(); previous() }
}

// reauthFlows lists pending reauth flow ids (best effort: nil on failure).
func (s *relaySession) reauthFlows() map[string]string {
	data, err := s.readFresh("ws", map[string]any{"type": "config_entries/flow/progress"})
	var flows []struct {
		FlowID  string `json:"flow_id"`
		Handler string `json:"handler"`
		Context struct {
			Source string `json:"source"`
		} `json:"context"`
	}
	if err != nil || json.Unmarshal(data, &flows) != nil {
		return nil
	}
	out := map[string]string{}
	for _, flow := range flows {
		if flow.Context.Source == "reauth" {
			out[flow.FlowID] = flow.Handler
		}
	}
	return out
}
