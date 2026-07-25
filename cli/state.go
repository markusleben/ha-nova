package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type installState struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	// ClientsVerifiedVersion records the version whose client integrations
	// (Hermes/Codex/OpenCode/Antigravity/Claude) were last (re)synced from the
	// canonical install root. It gates the post-update self-heal: a pre-0.6.1
	// binary never wrote it, so after a v0.6.0->v0.6.1 in-place update the marker
	// lags state.Version and the next command re-syncs once. Empty/omitted means
	// "unknown — verify on next run".
	ClientsVerifiedVersion string            `json:"clients_verified_version,omitempty"`
	InstallSource          string            `json:"install_source"`
	InstalledClients       []string          `json:"installed_clients"`
	ClientInstallModes     map[string]string `json:"client_install_modes"`
	PathManaged            bool              `json:"path_managed"`
	PathTarget             string            `json:"path_target"`
}

func loadState(paths runtimePaths) (installState, error) {
	data, err := os.ReadFile(paths.StateFile)
	if err != nil {
		return installState{}, err
	}

	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return installState{}, fmt.Errorf("HA NOVA state file is corrupt: %s. Move or remove %s, then rerun setup if needed", err, paths.StateFile)
	}
	return normalizeState(state), nil
}

func loadStateOrDefault(paths runtimePaths) installState {
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return defaultInstallState()
	}
	return state
}

func loadStateOrDefaultChecked(paths runtimePaths) (installState, error) {
	state, err := loadState(paths)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultInstallState(), nil
		}
		return installState{}, err
	}
	return state, nil
}

func defaultInstallState() installState {
	return installState{
		SchemaVersion:      stateSchemaVersion,
		ClientInstallModes: map[string]string{},
	}
}

func normalizeState(state installState) installState {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = stateSchemaVersion
	}
	if state.ClientInstallModes == nil {
		state.ClientInstallModes = map[string]string{}
	}
	state.InstallSource = normalizeInstallSource(state.InstallSource)
	state.InstalledClients = normalizeClients(state.InstalledClients)
	state.ClientInstallModes = normalizeClientInstallModes(state.ClientInstallModes)
	return state
}

func saveState(paths runtimePaths, state installState) error {
	state.SchemaVersion = stateSchemaVersion
	if state.ClientInstallModes == nil {
		state.ClientInstallModes = map[string]string{}
	}
	state.InstallSource = normalizeInstallSource(state.InstallSource)
	state.InstalledClients = normalizeClients(state.InstalledClients)
	state.ClientInstallModes = normalizeClientInstallModes(state.ClientInstallModes)
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		return err
	}
	return writeJSONFile(paths.StateFile, state, 0o600)
}

type bundleMetadata struct {
	BundleFormatVersion int                         `json:"bundle_format_version"`
	Version             string                      `json:"version"`
	OS                  string                      `json:"os"`
	Arch                string                      `json:"arch"`
	BinaryName          string                      `json:"binary_name"`
	CloudRelease        *cloudReleaseBundleEvidence `json:"cloud_release,omitempty"`
}

type cloudReleaseBundleEvidence struct {
	Schema        int    `json:"schema"`
	SourceTreeSHA string `json:"source_tree_sha"`
	BinarySHA256  string `json:"binary_sha256"`
	Signature     string `json:"signature"`
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

func writeJSONFile(path string, value interface{}, mode os.FileMode) error {
	return writeJSONFileOpts(path, value, mode, true)
}

// writeJSONFileNoHTMLEscape keeps <, >, & readable instead of \u003c-style
// escapes — for files parsed textually outside Go (the session-start hook
// greps the update cache's highlight text).
func writeJSONFileNoHTMLEscape(path string, value interface{}, mode os.FileMode) error {
	return writeJSONFileOpts(path, value, mode, false)
}

func writeJSONFileOpts(path string, value interface{}, mode os.FileMode, escapeHTML bool) error {
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
	enc.SetEscapeHTML(escapeHTML)
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
		value = canonicalClientID(value)
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

func normalizeClientInstallModes(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := map[string]string{}
	for key, value := range values {
		canonical := canonicalClientID(key)
		if canonical == "" {
			continue
		}
		if key == "gemini" {
			if _, exists := out[canonical]; exists {
				continue
			}
		}
		out[canonical] = value
	}
	return out
}

func canonicalClientID(value string) string {
	if value == "gemini" {
		return "antigravity"
	}
	return value
}

func mergeStateClients(state *installState, clients []string) {
	if state == nil {
		return
	}
	merged := append(append([]string{}, state.InstalledClients...), clients...)
	state.InstalledClients = normalizeClients(merged)
}

func isNotExist(err error) bool {
	return err != nil && errors.Is(err, os.ErrNotExist)
}
