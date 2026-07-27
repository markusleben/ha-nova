package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
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
		parts = append(parts, "authentication")
	}
	if s.RelayOK {
		parts = append(parts, "connection check")
	}
	if s.WSOK {
		parts = append(parts, "Home Assistant connection")
	}
	if s.SkillsOK {
		parts = append(parts, "skill installation")
	}
	return strings.Join(parts, ", ")
}

// deviceSetupState reports the install state over the paired-device transport.
// The second return is false for legacy installs (no device credential) — the
// legacy token path would wrongly report "no auth" for a paired device.
func deviceSetupState(paths runtimePaths, cfg runtimeConfig, state installState, target string) (setupState, bool) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	base, client, credential, device, err := relayFunctionalTransportForSetupState(
		ctx,
		cfg,
	)
	if err != nil || !device {
		return setupState{}, false
	}
	current := setupState{
		ConfigOK: setupConnectionConfigured(cfg),
		SkillsOK: clientsAppearInstalled(paths, target, state),
		TokenOK:  true,
	}
	readiness := checkRelayReadinessOverTransportContext(
		ctx,
		base,
		client,
		credential,
	)
	if readiness.HealthErr == nil {
		current.RelayOK = true
		current.WSOK = readiness.WSReady
	}
	return current, true
}

func detectSetupState(paths runtimePaths, cfg runtimeConfig, state installState, target string) setupState {
	if current, ok := deviceSetupState(paths, cfg, state, target); ok {
		return current
	}
	token, err := readRelayAuthToken()
	return detectSetupStateWithToken(paths, cfg, state, target, token, err == nil && strings.TrimSpace(token) != "")
}

// detectSetupStateForAssessment is the wizard's status view: device transport
// when paired, otherwise the already-read saved token. The token STAGE keeps
// calling detectSetupStateWithToken directly — its decisions are about the
// legacy token by definition.
func detectSetupStateForAssessment(paths runtimePaths, cfg runtimeConfig, state installState, target, savedToken string, hadSavedToken bool) setupState {
	if current, ok := deviceSetupState(paths, cfg, state, target); ok {
		return current
	}
	return detectSetupStateWithToken(paths, cfg, state, target, savedToken, hadSavedToken)
}

func detectSetupStateWithToken(paths runtimePaths, cfg runtimeConfig, state installState, target, token string, tokenOK bool) setupState {
	current := setupState{
		ConfigOK: setupConnectionConfigured(cfg),
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

func setupConnectionConfigured(cfg runtimeConfig) bool {
	localConfigured := strings.TrimSpace(cfg.HAHost) != "" &&
		strings.TrimSpace(cfg.HAURL) != "" &&
		strings.TrimSpace(cfg.RelayBaseURL) != ""
	cloudConfigured := effectiveRoutePolicy(cfg.RoutePolicy) == routePolicyCloud &&
		cfg.Cloud.ready() &&
		strings.TrimSpace(cfg.ProfileID) != "" &&
		strings.TrimSpace(cfg.RelayInstanceID) != ""
	return localConfigured || cloudConfigured
}

func relayFunctionalTransportForSetupState(
	ctx context.Context,
	cfg runtimeConfig,
) (string, *http.Client, string, bool, error) {
	selected, err := selectRelayTransport(ctx, cfg, "", false)
	if err != nil {
		return "", nil, "", false, err
	}
	return selected.BaseURL, selected.Client, selected.Credential, selected.DeviceMode, nil
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
