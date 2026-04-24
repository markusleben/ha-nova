package main

import (
	"encoding/json"
	"strings"
)

type setupState struct {
	ConfigOK bool
	TokenOK  bool
	RelayOK  bool
	WSOK     bool
	SkillsOK bool
}

func (s setupState) IsComplete() bool {
	return s.ConfigOK && s.TokenOK && s.RelayOK && s.WSOK && s.SkillsOK
}

func (s setupState) SkipSummary() string {
	parts := []string{}
	if s.ConfigOK {
		parts = append(parts, "app installation")
	}
	if s.TokenOK {
		parts = append(parts, "relay token")
	}
	if s.RelayOK {
		parts = append(parts, "connection check")
	}
	if s.WSOK {
		parts = append(parts, "access token")
	}
	if s.SkillsOK {
		parts = append(parts, "skill installation")
	}
	return strings.Join(parts, ", ")
}

func detectSetupState(paths runtimePaths, cfg runtimeConfig, state installState, target string) setupState {
	token, err := readRelayAuthToken()
	return detectSetupStateWithToken(paths, cfg, state, target, token, err == nil && strings.TrimSpace(token) != "")
}

func detectSetupStateWithToken(paths runtimePaths, cfg runtimeConfig, state installState, target, token string, tokenOK bool) setupState {
	current := setupState{
		ConfigOK: cfg.HAHost != "" && cfg.HAURL != "" && cfg.RelayBaseURL != "",
		SkillsOK: clientsAppearInstalled(paths, target, state),
	}

	if tokenOK && strings.TrimSpace(token) != "" {
		current.TokenOK = true
	}
	if !current.ConfigOK || !current.TokenOK {
		return current
	}

	readiness := checkRelayReadiness(cfg.RelayBaseURL, token)
	if readiness.HealthErr != nil {
		return current
	}
	current.RelayOK = true
	current.WSOK = readiness.WSReady
	return current
}

func relayHealthWSConnected(body []byte) bool {
	var envelope struct {
		Data struct {
			HAWSConnected *bool `json:"ha_ws_connected"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Data.HAWSConnected != nil {
			return *envelope.Data.HAWSConnected
		}
	}

	var flat struct {
		HAWSConnected *bool `json:"ha_ws_connected"`
	}
	if err := json.Unmarshal(body, &flat); err == nil {
		if flat.HAWSConnected != nil {
			return *flat.HAWSConnected
		}
	}
	return false
}

func clientsAppearInstalled(paths runtimePaths, target string, state installState) bool {
	clients, _, err := resolveSetupClients(paths, target)
	if err != nil {
		return false
	}
	for _, client := range clients {
		if !clientReadyNow(paths, state, client) {
			return false
		}
	}
	return true
}

func clientReadyNow(paths runtimePaths, state installState, client string) bool {
	entry, ok, err := findRegistryClient(paths, client)
	if err != nil || !ok {
		return false
	}
	return evaluateClientStatus(paths, state, entry).Ready
}

func containsClient(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
