package main

import (
	"errors"
	"os"
	"strings"
)

func resolveSetupRetirementProfile(paths runtimePaths) (string, error) {
	if requested, _ := requestedServerSelection(); requested != "" {
		if err := validateServerProfileName(requested); err != nil {
			return "", err
		}
		return requested, nil
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultServerProfileName, nil
		}
		return "", err
	}
	return resolveSelectedServerProfile(doc)
}

func setupRetirementCheckpointExistsWithUnresolvedProfile(
	paths runtimePaths,
) (bool, error) {
	entries, err := os.ReadDir(paths.ConfigDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	const prefix = ".device-retirement."
	const suffix = ".json"
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) ||
			!strings.HasSuffix(name, suffix) {
			continue
		}
		profile := strings.TrimSuffix(
			strings.TrimPrefix(name, prefix),
			suffix,
		)
		if validateServerProfileName(profile) == nil {
			return true, nil
		}
	}
	return false, nil
}
