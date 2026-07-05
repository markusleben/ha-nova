package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type clientRegistryEntry struct {
	ID          string                      `json:"id"`
	Label       string                      `json:"label"`
	AdapterKind string                      `json:"adapter_kind"`
	SupportedOS []string                    `json:"supported_os"`
	Setup       clientRegistrySetup         `json:"setup,omitempty"`
	PerOS       map[string]clientRegistryOS `json:"per_os,omitempty"`
}

type clientRegistrySetup struct {
	ServiceCredentials *clientRegistryServiceCredentials `json:"service_credentials,omitempty"`
}

type clientRegistryServiceCredentials struct {
	RecommendedWhen []string `json:"recommended_when"`
	Label           string   `json:"label"`
	Help            string   `json:"help"`
	Disabled        bool     `json:"disabled,omitempty"`
}

type clientRegistryOS struct {
	Setup *clientRegistrySetup `json:"setup,omitempty"`
}

type clientRegistryFile struct {
	RegistryFormatVersion int                   `json:"registry_format_version,omitempty"`
	Clients               []clientRegistryEntry `json:"clients"`
}

func loadClientRegistry(paths runtimePaths) ([]clientRegistryEntry, error) {
	path, err := locateClientRegistry(paths)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parseClientRegistry(data)
}

func parseClientRegistry(data []byte) ([]clientRegistryEntry, error) {
	var file clientRegistryFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("invalid client registry: %w", err)
	}
	if file.RegistryFormatVersion != 0 && file.RegistryFormatVersion != 1 {
		return nil, fmt.Errorf("invalid client registry: unsupported registry_format_version %d", file.RegistryFormatVersion)
	}
	if len(file.Clients) == 0 {
		return nil, fmt.Errorf("invalid client registry: no clients configured")
	}
	seen := map[string]bool{}
	for _, client := range file.Clients {
		if client.ID == "" || client.Label == "" || client.AdapterKind == "" || len(client.SupportedOS) == 0 {
			return nil, fmt.Errorf("invalid client registry entry: %+v", client)
		}
		if !isKnownClientRegistryAdapter(client.AdapterKind) {
			return nil, fmt.Errorf("invalid client registry: unknown adapter %q for %s", client.AdapterKind, client.ID)
		}
		if seen[client.ID] {
			return nil, fmt.Errorf("invalid client registry: duplicate client id %q", client.ID)
		}
		seen[client.ID] = true
		for _, osName := range client.SupportedOS {
			if !isKnownClientRegistryOS(osName) {
				return nil, fmt.Errorf("invalid client registry: unknown OS %q for %s", osName, client.ID)
			}
		}
		if err := validateClientRegistrySetup(client.ID, client.Setup); err != nil {
			return nil, err
		}
		for osName, override := range client.PerOS {
			if !isKnownClientRegistryOS(osName) {
				return nil, fmt.Errorf("invalid client registry: unknown per_os key %q for %s", osName, client.ID)
			}
			if override.Setup != nil {
				if err := validateClientRegistrySetup(client.ID, *override.Setup); err != nil {
					return nil, err
				}
			}
		}
	}
	return file.Clients, nil
}

func validateClientRegistryFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = parseClientRegistry(data)
	return err
}

func locateClientRegistry(paths runtimePaths) (string, error) {
	candidates := []string{}
	for _, root := range sourceRootCandidates(paths) {
		candidates = append(candidates, filepath.Join(root, "clients", "registry.json"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("client registry missing")
}

func findRegistryClient(paths runtimePaths, id string) (clientRegistryEntry, bool, error) {
	id = canonicalClientID(id)
	clients, err := loadClientRegistry(paths)
	if err != nil {
		return clientRegistryEntry{}, false, err
	}
	for _, client := range clients {
		if client.ID == id {
			return client, true, nil
		}
	}
	return clientRegistryEntry{}, false, nil
}

func clientSupportedOnCurrentOS(client clientRegistryEntry) bool {
	for _, supported := range client.SupportedOS {
		if supported == bundlePlatformOS() {
			return true
		}
	}
	return false
}

func isKnownClientRegistryOS(value string) bool {
	switch value {
	case "macos", "linux", "windows":
		return true
	default:
		return false
	}
}

func isKnownClientRegistryAdapter(value string) bool {
	switch value {
	case "skill_tree", "skill_flat", "plugin_marketplace":
		return true
	default:
		return false
	}
}

func validateClientRegistrySetup(clientID string, setup clientRegistrySetup) error {
	if setup.ServiceCredentials == nil {
		return nil
	}
	serviceCredentials := setup.ServiceCredentials
	if serviceCredentials.Disabled {
		return nil
	}
	if serviceCredentials.Label == "" {
		return fmt.Errorf("invalid client registry: setup.service_credentials.label missing for %s", clientID)
	}
	if serviceCredentials.Help == "" {
		return fmt.Errorf("invalid client registry: setup.service_credentials.help missing for %s", clientID)
	}
	if len(serviceCredentials.RecommendedWhen) == 0 {
		return fmt.Errorf("invalid client registry: setup.service_credentials.recommended_when missing for %s", clientID)
	}
	for _, value := range serviceCredentials.RecommendedWhen {
		switch value {
		case "headless", "locked_keyring", "systemd_user_service":
		default:
			return fmt.Errorf("invalid client registry: unknown service credential recommendation %q for %s", value, clientID)
		}
	}
	return nil
}

func clientSetupForCurrentOS(client clientRegistryEntry) clientRegistrySetup {
	setup := client.Setup
	if override, ok := client.PerOS[bundlePlatformOS()]; ok && override.Setup != nil {
		setup = *override.Setup
	}
	return setup
}

func clientSupportsServiceCredentials(client clientRegistryEntry) bool {
	serviceCredentials := clientSetupForCurrentOS(client).ServiceCredentials
	return serviceCredentials != nil && !serviceCredentials.Disabled
}

func selectedClientsSupportServiceCredentials(paths runtimePaths, selectedClients []string) (clientRegistryEntry, bool, error) {
	clients, err := loadClientRegistry(paths)
	if err != nil {
		return clientRegistryEntry{}, false, err
	}
	selected := map[string]bool{}
	for _, client := range selectedClients {
		selected[client] = true
	}
	for _, client := range clients {
		if selected[client.ID] && clientSupportsServiceCredentials(client) {
			return client, true, nil
		}
	}
	return clientRegistryEntry{}, false, nil
}
