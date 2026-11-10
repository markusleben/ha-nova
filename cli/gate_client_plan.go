package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Where the gate meets plans and apply (spec → CLI client): a typed plan on
// a gated App is a relay plan, and apply asks the Relay for its first
// requests before it sends anything.

// gateChannel: a typed plan on a gated App is confirmed through the Relay
// (channelRelay). A raw plan only a skill's typed mark makes typed keeps
// this computer's asks (the gate may pass it), and so do dashboard and rooms
// plans, whose requests apply can't name before it sends them. "" without
// a gate, as on an older App.
func gateChannel(e *tierEngine, plan storedPlan) (string, error) {
	on, err := e.s.gateOn()
	local := plan.Kind == "raw" && !e.requestTyped || plan.Kind == "rooms" || plan.ConfigAction != nil && plan.ConfigAction.Domain == "dashboard"
	if err != nil || !on || local {
		return "", err
	}
	return channelRelay, nil
}

// gateBeforeSend runs before a typed plan's first write (applyPlan): a
// profile that saw the gate refuses once it is gone, and a relay plan asks
// for the requests it sends first and waits for the person. Later requests
// ask when the Relay holds them (sendWrite).
func gateBeforeSend(s *relaySession, channel string, fresh storedPlan, batch batchReplan) error {
	if fresh.Tier != tierTyped.String() {
		return nil
	}
	on, err := s.gateOn()
	switch {
	case err != nil || channel != channelRelay:
		return err
	case !on:
		return gateRolledBack()
	}
	reqs := gateFirstRequests(fresh, batch)
	if len(reqs) == 0 {
		return nil
	}
	if err := s.gateAsk(reqs); err != nil {
		return err
	}
	humanWhere = channelRelay
	return nil
}

// gateFirstRequests: what a relay plan sends first, byte for byte as its
// executor sends it (executeAction, executeConfig, executeRaw); a batch's
// first member when that one is typed.
func gateFirstRequests(fresh storedPlan, batch batchReplan) []gateRequest {
	switch {
	case fresh.Kind == "call":
		return []gateRequest{coreRequest(http.MethodPost, servicePath(fresh.Action.Service, fresh.Action.Response), serviceBody(fresh.Action))}
	case fresh.Kind == "raw":
		var reqs []gateRequest
		for _, item := range fresh.RawRequests {
			body := []byte(item.Payload)
			if item.Endpoint == "backups" { // as guardRaw sends it
				body = redactSecrets(body, false)
			}
			reqs = append(reqs, gateRequest{item.Endpoint, body})
		}
		return reqs
	case fresh.Kind == "batch" && len(batch.members) > 0 && batch.members[0].fresh.Tier == tierTyped.String():
		return configRequests(*batch.members[0].fresh.ConfigAction)
	case fresh.ConfigAction != nil:
		return configRequests(*fresh.ConfigAction)
	}
	return nil
}

// configRequests: an automation or script write or delete and its reload.
func configRequests(action configAction) []gateRequest {
	if action.Domain == "dashboard" {
		return nil
	}
	write := coreRequest(http.MethodDelete, action.Path, nil)
	if action.Op != "delete" {
		write = coreRequest(http.MethodPost, action.Path, action.Body)
	}
	return append([]gateRequest{write}, gateFollowUps(write)...)
}

// gateFollowUps: the reload apply sends after a config write (executeConfig),
// so one approval covers both (spec §4).
func gateFollowUps(r gateRequest) []gateRequest {
	var core struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	if r.endpoint != "core" || json.Unmarshal(r.body, &core) != nil || core.Method == http.MethodGet || !configPathPattern.MatchString(core.Path) {
		return nil
	}
	parts := strings.Split(core.Path, "/") // "", api, config, <domain>, config, <id>
	return []gateRequest{coreRequest(http.MethodPost, servicePath(parts[3]+".reload", false), configReloadData(parts[3], parts[5]))}
}

// coreRequest: one core write as s.write sends it.
func coreRequest(method, path string, body any) gateRequest {
	data, _ := json.Marshal(corePayload(method, path, body))
	return gateRequest{"core", data}
}

// gateOn: whether the Relay gates typed requests (GET /health gate: 1), read
// once per session. A profile that saw the gate (gate_seen) refuses once it
// is gone; one that never saw it keeps today's asks, as on an older App,
// also when the read fails (a gated Relay still holds a typed request).
func (s *relaySession) gateOn() (bool, error) {
	if s.gateExempt() {
		return false, nil
	}
	s.mu.Lock()
	known, on := s.gate != nil && s.gate.checked, s.gate != nil && s.gate.on
	s.mu.Unlock()
	if known {
		return on, nil
	}
	on, err := s.readGate()
	seen := gateSeen(s.paths, s.server)
	switch {
	case err != nil && seen:
		return false, gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't read whether its safety check in Home Assistant is on ("+err.Error()+"); nothing was sent",
			"Tell the user nothing changed; they can try again in a moment. If it stays, they check that the NOVA App runs in Home Assistant.")
	case !on && seen:
		return false, gateRolledBack()
	case on && !seen:
		markGateSeen(s.paths, s.server)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate == nil {
		s.gate = &relayGate{}
	}
	s.gate.checked, s.gate.on = true, on
	return on, nil
}

// readGate reads the Relay's /health once: gate 1 means it gates.
func (s *relaySession) readGate() (bool, error) {
	s.mu.Lock()
	parent := s.ctx
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, relaySessionRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.transport.BaseURL, "/")+"/health", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+s.transport.Credential)
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	var health struct {
		OK   bool `json:"ok"`
		Data struct {
			Gate int `json:"gate"`
		} `json:"data"`
	}
	if err != nil || resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &health) != nil || !health.OK {
		return false, fmt.Errorf("the Relay's health answer (HTTP %d) can't be read", resp.StatusCode)
	}
	return health.Data.Gate == 1, nil
}

// gateSeen: this server profile once saw the gate (config.json; user-writable,
// so it guards against an accidental rollback, not an attacker: spec §6).
func gateSeen(paths runtimePaths, server string) bool {
	doc, err := loadConfigDocument(paths.ConfigFile)
	var profile struct {
		GateSeen bool `json:"gate_seen"`
	}
	return err == nil && doc.servers != nil && json.Unmarshal(doc.servers[server], &profile) == nil && profile.GateSeen
}

// markGateSeen records gate_seen in the profile, best effort: a failed write
// only means the next session checks again.
func markGateSeen(paths runtimePaths, server string) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil || !doc.hasProfile(server) {
		return
	}
	servers, err := documentServersCopy(doc)
	var profile map[string]json.RawMessage
	if err != nil || json.Unmarshal(servers[server], &profile) != nil || profile == nil {
		return
	}
	profile["gate_seen"] = json.RawMessage("true")
	if servers[server], err = json.Marshal(profile); err == nil {
		_ = writeServersDocument(paths, doc, servers, doc.defaultServerName())
	}
}
