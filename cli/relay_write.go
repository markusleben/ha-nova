package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"
)

// The only write surface of the typed verbs (spec §2.3, I7): each request
// is sent exactly once, never retried, never through the read path (read()
// refuses it, I1), and must pass checkWriteRequest — service calls, the
// automation/script config endpoints, the dashboard save/create/delete WS
// commands, one device's area (rooms plans), and a config-snapshot save.

var relayWriteTimeout = 30 * time.Second

var (
	servicePathPattern  = regexp.MustCompile(`^/api/services/[a-z0-9_]+/[a-z0-9_]+(\?return_response)?$`)
	configPathPattern   = regexp.MustCompile(`^/api/config/(automation|script)/config/[A-Za-z0-9_-]+$`)
	snapshotNamePattern = regexp.MustCompile(`^auto-[a-z0-9-]{1,59}$`)
)

// checkWriteRequest is the write allowlist; anything else is refused before
// sending. Tests use the same function as their oracle.
func checkWriteRequest(endpoint string, payload map[string]any) error {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	_, hasBody := payload["body"]
	switch {
	case endpoint == "core" && method == http.MethodPost && hasBody && len(payload) == 3 &&
		(servicePathPattern.MatchString(path) || configPathPattern.MatchString(path)):
		return nil
	case endpoint == "core" && method == http.MethodDelete && !hasBody && len(payload) == 2 && configPathPattern.MatchString(path):
		return nil
	case endpoint == "backups" && payload["action"] == "save" && len(payload) == 4 && payload["data"] != nil &&
		(payload["category"] == "automations" || payload["category"] == "scripts" || payload["category"] == "dashboards") &&
		snapshotNamePattern.MatchString(fmt.Sprint(payload["name"])):
		return nil
	case endpoint == "ws" && dashboardWrite(payload):
		return nil
	case endpoint == "ws" && deviceAreaUpdate(payload):
		return nil
	}
	return fmt.Errorf("refused a write outside the typed-verb allowlist (%s %s %s); nothing was sent", endpoint, method, path)
}

// dashboardWrite: one dashboard save, create, or delete with exactly the
// fields the CLI sends (dashboard-write spec).
func dashboardWrite(payload map[string]any) bool {
	required := map[any][]string{"lovelace/config/save": {"type", "config"}, "lovelace/dashboards/create": {"type", "url_path", "title"},
		"lovelace/dashboards/delete": {"type", "dashboard_id"}}[payload["type"]]
	optional := map[any][]string{"lovelace/config/save": {"url_path"}, "lovelace/dashboards/create": {"icon", "require_admin", "show_in_sidebar"}}[payload["type"]]
	known := 0
	for _, key := range required {
		if _, present := payload[key]; !present {
			return false
		}
		known++
	}
	for _, key := range optional {
		if _, present := payload[key]; present {
			known++
		}
	}
	for key, value := range payload { // flags are booleans; every other field but the config is a non-empty string
		_, isFlag := value.(bool)
		text, isText := value.(string)
		if key == "require_admin" || key == "show_in_sidebar" {
			if !isFlag {
				return false
			}
		} else if key != "config" && (!isText || text == "") {
			return false
		}
	}
	return required != nil && known == len(payload)
}

// deviceAreaUpdate: one device's area and nothing else of the device
// (rooms plans); area_id null takes the device out of its area.
func deviceAreaUpdate(payload map[string]any) bool {
	device, _ := payload["device_id"].(string)
	area, isText := payload["area_id"].(string)
	_, hasArea := payload["area_id"]
	return payload["type"] == deviceAreaRequest && len(payload) == 3 && device != "" && hasArea && (payload["area_id"] == nil || isText && area != "")
}

// serviceResult is Home Assistant's own answer to a core write.
type serviceResult struct {
	status int
	body   json.RawMessage
}

// postService sends one service call; response asks for its
// service_response (?return_response).
func (s *relaySession) postService(service string, body map[string]any, response bool) (serviceResult, error) {
	domain, name := splitID(service)
	path := "/api/services/" + domain + "/" + name
	if response {
		path += "?return_response"
	}
	return s.writeCore(http.MethodPost, path, body)
}

// writeCore sends one core write. Errors are either a relayCallError / plain
// error (nothing reached Home Assistant: exit 1) or a
// relayOutcomeUnknownError (it may have run: exit 7, never retry).
func (s *relaySession) writeCore(method, path string, body any) (serviceResult, error) {
	payload := map[string]any{"method": method, "path": path}
	if body != nil {
		payload["body"] = body
	}
	data, err := s.write("core", payload)
	if err != nil {
		return serviceResult{}, err
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if json.Unmarshal(data, &core) != nil || core.Status == 0 {
		return serviceResult{}, relayPostRequestOutcomeUnknownError("reading the upstream status", errors.New("no upstream status"))
	}
	return serviceResult{status: core.Status, body: core.Body}, nil
}

// saveConfigSnapshot stores one pre-delete config snapshot in the Relay's
// /backups store and returns its file reference.
func (s *relaySession) saveConfigSnapshot(category, name string, config any) (string, error) {
	data, err := s.write("backups", map[string]any{"action": "save", "category": category, "name": name, "data": config})
	if err != nil {
		return "", err
	}
	var saved struct {
		File string `json:"file"`
	}
	if json.Unmarshal(data, &saved) != nil || saved.File == "" {
		return "", errors.New("the Relay did not report a snapshot file")
	}
	return saved.File, nil
}

// write sends one allowlisted request and returns the envelope data.
func (s *relaySession) write(endpoint string, payload map[string]any) (json.RawMessage, error) {
	if err := checkWriteRequest(endpoint, payload); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), relayWriteTimeout)
	defer cancel()
	req, err := newRelayRequest(ctx, s.transport, endpoint, body)
	if err != nil {
		return nil, err
	}
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return nil, relayRequestOutcomeUnknownError(s.transport.BaseURL, err)
	}
	defer resp.Body.Close()
	if isHTTPRedirect(resp.StatusCode) {
		return nil, relayHTTPOutcomeUnknownError(resp.StatusCode)
	}
	if s.transport.Via == relayViaLocal && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		return nil, errors.New(localRelayAuthRepairMessage(resp.StatusCode, s.server, s.cfg.RelayBaseURL))
	}
	raw, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err == nil {
		raw, err = normalizeUTF8Bytes(raw, "Relay response")
	}
	if err != nil {
		return nil, relayPostRequestOutcomeUnknownError("reading the Relay response", err)
	}
	raw = redactPhoneSecrets(raw, true)
	var envelope struct {
		OK    *bool           `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.OK == nil {
		return nil, relayPostRequestOutcomeUnknownError("validating the Relay result envelope", errors.New("not a valid result envelope"))
	}
	if !*envelope.OK {
		// Home Assistant answered a WS command with its own error: rejected,
		// not unknown (the Relay wraps it as 502 UPSTREAM_WS_COMMAND_ERROR).
		rejectedByHA := endpoint == "ws" && envelope.Error.Code == "UPSTREAM_WS_COMMAND_ERROR"
		if resp.StatusCode >= http.StatusInternalServerError && !rejectedByHA { // upstream error or timeout: it may have run
			return nil, relayHTTPOutcomeUnknownError(resp.StatusCode)
		}
		return nil, &relayCallError{endpoint: endpoint, status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	return envelope.Data, nil
}

// readFresh bypasses the memo — verification polls and the reauth re-read
// must observe the state after the write — under the same allowlist (I1),
// bounded by timeout.
func (s *relaySession) readFresh(endpoint string, payload map[string]any, timeout time.Duration) (json.RawMessage, error) {
	if err := checkReadOnlyRequest(endpoint, payload); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return s.sendWithin(endpoint, body, timeout)
}

func (s *relaySession) stateFresh(entityID string, timeout time.Duration) (*entityState, error) {
	data, err := s.readFresh("core", map[string]any{"method": http.MethodGet, "path": "/api/states/" + entityID}, timeout)
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

// reauthSnapshotTimeout bounds the best-effort snapshot so it never delays
// the confirmed call by more than a few seconds.
var reauthSnapshotTimeout = 5 * time.Second

type reauthFlow struct{ handler, entryID string }

// reauthFlows lists pending reauth flows by id (best effort: nil on failure).
func (s *relaySession) reauthFlows() map[string]reauthFlow {
	data, err := s.readFresh("ws", map[string]any{"type": "config_entries/flow/progress"}, reauthSnapshotTimeout)
	var flows []struct {
		FlowID  string `json:"flow_id"`
		Handler string `json:"handler"`
		Context struct {
			Source  string `json:"source"`
			EntryID string `json:"entry_id"`
		} `json:"context"`
	}
	if err != nil || json.Unmarshal(data, &flows) != nil {
		return nil
	}
	out := map[string]reauthFlow{}
	for _, flow := range flows {
		if flow.Context.Source == "reauth" {
			out[flow.FlowID] = reauthFlow{handler: flow.Handler, entryID: flow.Context.EntryID}
		}
	}
	return out
}
