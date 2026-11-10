package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Where the gate meets plans and apply (spec → CLI client): a typed plan on
// a gated App is a relay plan, and apply asks the Relay for its first
// requests before it sends anything.

// gateChannel: a typed plan on a gated App is confirmed through the Relay
// (channelRelay). A raw plan with an item only a skill's typed mark makes
// typed keeps this computer's asks (the Relay neither sees nor needs the
// mark; an item typed by itself meets the gate when the Relay holds it, as
// on any such plan), and so do dashboard and rooms plans, whose requests
// apply can't name before it sends them. "" without a gate, as on an older App.
func gateChannel(e *tierEngine, plan storedPlan) (string, error) {
	on, err := e.s.gateOn()
	local := plan.Kind == "raw" && e.markTyped || plan.Kind == "rooms" || plan.ConfigAction != nil && plan.ConfigAction.Domain == "dashboard"
	if err != nil || !on || local {
		return "", err
	}
	return channelRelay, nil
}

// gateBeforeSend runs before a typed plan's first write (applyPlan): a
// profile that saw the gate refuses once it is gone, and a relay plan asks
// for the requests it sends first and waits for the person. It answers the
// requests the person approved (none: nothing was asked). Later requests
// ask when the Relay holds them (sendWrite).
func gateBeforeSend(s *relaySession, channel string, fresh storedPlan, batch batchReplan) ([]gateRequest, error) {
	if fresh.Tier != tierTyped.String() {
		return nil, nil
	}
	on, err := s.gateOn()
	switch {
	case err != nil || channel != channelRelay:
		return nil, err
	case !on:
		return nil, gateRolledBack()
	}
	reqs := gateFirstRequests(fresh, batch)
	if len(reqs) == 0 {
		return nil, nil
	}
	if err := s.gateAsk(reqs); err != nil {
		return nil, err
	}
	humanWhere = channelRelay
	return reqs, nil
}

// gateStillApproved: after the person approved (which may take minutes),
// the plan checked again right before the first send (again, from
// recheck) is still the one they approved, unexpired, and sends the very
// bytes they approved. Otherwise nothing is sent and the grant expires
// unused; the plan is gone, as after any drift.
func gateStillApproved(stored, again storedPlan, err error, asked, now []gateRequest, expires time.Time) error {
	same := slices.EqualFunc(asked, now, func(a, b gateRequest) bool { return a.endpoint == b.endpoint && bytes.Equal(a.body, b.body) })
	switch {
	case err != nil || !stillConfirmed(stored, again) || again.Tier != stored.Tier || !same:
		return &verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
			message: "something changed in Home Assistant while the user confirmed; nothing was sent and the plan is gone",
			next:    "Tell the user nothing changed because the home changed meanwhile. Plan it again only if they still want it; it shows the new preview and asks them again."}
	case !verbNow().Before(expires):
		return refused("PLAN_EXPIRED", "plan "+stored.ID+" expired while the user confirmed; nothing was sent")
	}
	return nil
}

// gateFirstRequests: what a relay plan sends first, byte for byte as its
// executor sends it (executeAction, executeConfig, executeRaw); for a
// batch, every member's write, so one approval covers it.
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
	case fresh.Kind == "batch":
		var reqs []gateRequest
		for _, m := range batch.members {
			reqs = append(reqs, configRequests(*m.fresh.ConfigAction)...)
		}
		return reqs
	case fresh.ConfigAction != nil:
		return configRequests(*fresh.ConfigAction)
	}
	return nil
}

// configRequests: an automation or script write, or a delete and its reload.
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

// gateFollowUps: the reload apply sends after a config delete (executeConfig),
// so one approval covers both (spec §4). A save needs none: Home Assistant
// reloads after it on its own.
func gateFollowUps(r gateRequest) []gateRequest {
	var core struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	if r.endpoint != "core" || json.Unmarshal(r.body, &core) != nil || core.Method != http.MethodDelete || !configPathPattern.MatchString(core.Path) {
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
		if err := markGateSeen(s.paths, s.server); err != nil { // unrecorded, a rollback would go unnoticed: fail closed, and try again next time
			return false, gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't save in its settings file that the NOVA App's safety check is on ("+err.Error()+"); nothing was sent",
				"Tell the user nothing changed: HA NOVA couldn't save its settings file on this computer. They check that HA NOVA's settings folder is writable (ha-nova doctor shows where it is), then ask again.")
		}
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

// markGateSeen records gate_seen in the profile. A profile the settings
// file doesn't hold has nowhere to record it (nil); any other failure is
// an error, so a rollback can't go unnoticed (gateOn refuses).
func markGateSeen(paths runtimePaths, server string) error {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return err
	}
	if !doc.hasProfile(server) {
		return nil
	}
	servers, err := documentServersCopy(doc)
	if err != nil {
		return err
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(servers[server], &profile); err != nil || profile == nil {
		return fmt.Errorf("server profile %q can't be read", server)
	}
	profile["gate_seen"] = json.RawMessage("true")
	if servers[server], err = json.Marshal(profile); err != nil {
		return err
	}
	return writeServersDocument(paths, doc, servers, doc.defaultServerName())
}
