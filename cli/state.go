package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type installState struct {
	SchemaVersion      int               `json:"schema_version"`
	Version            string            `json:"version"`
	InstallSource      string            `json:"install_source"`
	InstalledClients   []string          `json:"installed_clients"`
	ClientInstallModes map[string]string `json:"client_install_modes"`
	PathManaged        bool              `json:"path_managed"`
	PathTarget         string            `json:"path_target"`
}

func loadState(paths runtimePaths) (installState, error) {
	data, err := os.ReadFile(paths.StateFile)
	if err != nil {
		return installState{}, err
	}

	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return installState{}, err
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = stateSchemaVersion
	}
	if state.ClientInstallModes == nil {
		state.ClientInstallModes = map[string]string{}
	}
	sort.Strings(state.InstalledClients)
	return state, nil
}

func loadStateOrDefault(paths runtimePaths) installState {
	state, err := loadState(paths)
	if err != nil {
		return installState{
			SchemaVersion:      stateSchemaVersion,
			ClientInstallModes: map[string]string{},
		}
	}
	return state
}

func saveState(paths runtimePaths, state installState) error {
	state.SchemaVersion = stateSchemaVersion
	if state.ClientInstallModes == nil {
		state.ClientInstallModes = map[string]string{}
	}
	sort.Strings(state.InstalledClients)
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		return err
	}
	return writeJSONFile(paths.StateFile, state, 0o600)
}

type bundleMetadata struct {
	BundleFormatVersion int    `json:"bundle_format_version"`
	Version             string `json:"version"`
	OS                  string `json:"os"`
	Arch                string `json:"arch"`
	BinaryName          string `json:"binary_name"`
}

func loadBundleMetadata(paths runtimePaths) (bundleMetadata, error) {
	data, err := os.ReadFile(paths.BundleFile)
	if err != nil {
		return bundleMetadata{}, err
	}
	var meta bundleMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return bundleMetadata{}, err
	}
	return meta, nil
}

func loadBundleMetadataOrDefault(paths runtimePaths) bundleMetadata {
	meta, err := loadBundleMetadata(paths)
	if err == nil {
		return meta
	}
	return bundleMetadata{
		BundleFormatVersion: bundleFormatVersion,
		Version:             localVersion(paths),
		OS:                  bundlePlatformOS(),
		Arch:                bundlePlatformArch(),
		BinaryName:          publicBinaryName(),
	}
}

func saveBundleMetadata(paths runtimePaths, meta bundleMetadata) error {
	meta.BundleFormatVersion = bundleFormatVersion
	if meta.BinaryName == "" {
		meta.BinaryName = publicBinaryName()
	}
	if meta.OS == "" {
		meta.OS = bundlePlatformOS()
	}
	if meta.Arch == "" {
		meta.Arch = bundlePlatformArch()
	}
	if err := os.MkdirAll(filepath.Dir(paths.BundleFile), 0o755); err != nil {
		return err
	}
	return writeJSONFile(paths.BundleFile, meta, 0o644)
}

func writeJSONFile(path string, value interface{}, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func normalizeClients(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStateClients(state *installState, clients []string) {
	if state == nil {
		return
	}
	merged := append(append([]string{}, state.InstalledClients...), clients...)
	state.InstalledClients = normalizeClients(merged)
}

func stateExists(paths runtimePaths) bool {
	_, err := os.Stat(paths.StateFile)
	return err == nil
}

func isNotExist(err error) bool {
	return err != nil && errors.Is(err, os.ErrNotExist)
}
