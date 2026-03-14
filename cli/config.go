package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type runtimeConfig struct {
	SchemaVersion int    `json:"schema_version"`
	HAHost        string `json:"ha_host"`
	HAURL         string `json:"ha_url"`
	RelayBaseURL  string `json:"relay_base_url"`
}

type config = runtimeConfig

func loadRuntimeConfig(pathArgs ...runtimePaths) (runtimeConfig, error) {
	paths, err := resolveRuntimePaths(pathArgs...)
	if err != nil {
		return runtimeConfig{}, err
	}

	cfg, err := loadJSONConfig(paths.ConfigFile)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	if cfg.RelayBaseURL == "" {
		return runtimeConfig{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	return cfg, nil
}

func loadConfig(pathArgs ...runtimePaths) (config, error) {
	return loadRuntimeConfig(pathArgs...)
}

func loadJSONConfig(path string) (runtimeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runtimeConfig{}, err
	}

	var cfg runtimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return runtimeConfig{}, err
	}
	return cfg, nil
}

func resolveRuntimePaths(pathArgs ...runtimePaths) (runtimePaths, error) {
	if len(pathArgs) > 0 {
		return pathArgs[0], nil
	}
	return detectPaths()
}

func saveConfig(paths runtimePaths, cfg runtimeConfig) error {
	cfg.SchemaVersion = configSchemaVersion
	return writeJSONFile(paths.ConfigFile, cfg, 0o600)
}
