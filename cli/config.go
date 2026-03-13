package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	RelayBaseURL string
}

func loadConfig() (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, fmt.Errorf("cannot determine home directory: %w", err)
	}

	path := filepath.Join(home, ".config", "ha-nova", "onboarding.env")
	f, err := os.Open(path)
	if err != nil {
		return config{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	defer f.Close()

	var cfg config
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, "'\"")
		switch key {
		case "RELAY_BASE_URL":
			cfg.RelayBaseURL = value
		}
	}
	if err := scanner.Err(); err != nil {
		return config{}, err
	}
	if cfg.RelayBaseURL == "" {
		return config{}, fmt.Errorf("RELAY_BASE_URL not found in %s", path)
	}
	return cfg, nil
}
