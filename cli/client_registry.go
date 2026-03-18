package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type clientRegistryEntry struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	AdapterKind string   `json:"adapter_kind"`
	SupportedOS []string `json:"supported_os"`
}

type clientRegistryFile struct {
	Clients []clientRegistryEntry `json:"clients"`
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

	var file clientRegistryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("invalid client registry: %w", err)
	}
	if len(file.Clients) == 0 {
		return nil, fmt.Errorf("invalid client registry: no clients configured")
	}
	for _, client := range file.Clients {
		if client.ID == "" || client.Label == "" || client.AdapterKind == "" || len(client.SupportedOS) == 0 {
			return nil, fmt.Errorf("invalid client registry entry: %+v", client)
		}
	}
	return file.Clients, nil
}

func locateClientRegistry(paths runtimePaths) (string, error) {
	candidates := []string{
		filepath.Join(resolveSourceRoot(paths), "clients", "registry.json"),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "clients", "registry.json"),
			filepath.Join(cwd, "..", "clients", "registry.json"),
		)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("client registry missing")
}

func findRegistryClient(paths runtimePaths, id string) (clientRegistryEntry, bool, error) {
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
