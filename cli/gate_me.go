package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// What the App says of this computer's window (GET /gate/v1/me, 0.32 spec
// §4, §6): window when it may confirm there now, else why not. The agent's
// texts and the waiting window follow it; only an ask's channels let the
// window confirm (gate_window_confirm.go).

const gateMeRoute = "/gate/v1/me"

// gateWindowState: why "" when it couldn't be read (an older App, a failed
// read, a chat app): today's texts.
type gateWindowState struct {
	window bool
	why    string
}

// gateMe reads it once per session, within afterCheck, as readGate does.
func (s *relaySession) gateMe() gateWindowState {
	s.mu.Lock()
	if s.gate != nil && s.gate.me != nil {
		defer s.mu.Unlock()
		return *s.gate.me
	}
	s.mu.Unlock()
	me := s.readGateMe()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate == nil {
		s.gate = &relayGate{}
	}
	s.gate.me = &me
	return me
}

func (s *relaySession) readGateMe() gateWindowState {
	s.mu.Lock()
	on := s.gate != nil && s.gate.checked && s.gate.on // read already (gateOn): a gated App
	s.mu.Unlock()
	if !on || s.gateChat() {
		return gateWindowState{}
	}
	ctx, cancel := context.WithTimeout(planAsks, afterCheck)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.transport.BaseURL, "/")+gateMeRoute, nil)
	if err != nil {
		return gateWindowState{}
	}
	req.Header.Set("Authorization", "Bearer "+s.transport.Credential)
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return gateWindowState{}
	}
	defer resp.Body.Close()
	raw, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	var me struct {
		OK   bool `json:"ok"`
		Data struct {
			Window bool   `json:"window"`
			Why    string `json:"why"`
		} `json:"data"`
	}
	if err != nil || resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &me) != nil || !me.OK {
		return gateWindowState{}
	}
	switch me.Data.Why { // a closed set: anything else reads as unread
	case "on", "off", "device", "pair_again", "file", "legacy_cleanup_pending", "not_yours", "owner":
		return gateWindowState{window: me.Data.Window && me.Data.Why == "on", why: me.Data.Why}
	}
	return gateWindowState{}
}

// windowHere: this invocation can show a window (not a chat app, windows
// not turned off, a display), so texts may name it.
func (s *relaySession) windowHere() bool {
	return !s.gateChat() && !windowOff(s.paths) && windowAvailable()
}

// gateWindowWhy: the one line why this computer's window can't confirm, ""
// when none applies (on, a service install, unread).
func gateWindowWhy(lang, why, server string) string {
	switch why {
	case "off":
		return confirmT(lang, "gate.why_off")
	case "not_yours":
		return confirmT(lang, "gate.why_not_yours")
	case "pair_again":
		return confirmT(lang, "gate.why_pair_again")
	case "file":
		return confirmT(lang, "gate.why_file")
	case "owner":
		return confirmT(lang, "gate.why_owner")
	case "legacy_cleanup_pending":
		return confirmT(lang, "gate.why_cleanup", "command", gateCleanupCommand(server))
	}
	return ""
}

// gateCleanupCommand: the retry that finishes signing the old version out
// (spec §5.3); it never starts by itself.
func gateCleanupCommand(server string) string {
	if serverProfileNamePattern.MatchString(server) {
		return "ha-nova setup --server " + server
	}
	return "ha-nova setup"
}
