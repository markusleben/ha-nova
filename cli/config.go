package main

import (
	"fmt"
)

type runtimeConfig struct {
	SchemaVersion  int    `json:"schema_version"`
	HAHost         string `json:"ha_host"`
	HAURL          string `json:"ha_url"`
	RelayBaseURL   string `json:"relay_base_url"`
	RelayTokenFile string `json:"relay_token_file,omitempty"`
	// Stable, non-secret identifier for this OS-user installation. All AI clients
	// on the same machine share one device credential keyed by this id; the relay
	// records it so re-pairing replaces the same install's credential. Install-
	// wide: server profiles share it (see config_profiles.go).
	ClientInstallID string `json:"client_install_id,omitempty"`
	// Secure device endpoint learned from pairing: the pinned TLS base URL and the
	// exact SHA-256 SPKI pin. Functional device calls go here; a pin change forces
	// re-pairing.
	RelaySecureBaseURL string `json:"relay_secure_base_url,omitempty"`
	RelaySpkiPin       string `json:"relay_spki_pin,omitempty"`
	// A secure endpoint learned during an in-progress pairing, promoted to the live
	// fields above only after activation succeeds. Kept separate so a failed re-pair
	// never overwrites the working endpoint, while resumePendingActivation can still
	// find the endpoint after a crash between activation and promotion.
	PendingSecureBaseURL string `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string `json:"pending_spki_pin,omitempty"`
}

type config = runtimeConfig

// loadRuntimeConfig returns the flat runtimeConfig of the SELECTED server
// profile (--server flag > HA_NOVA_SERVER env > default_server), so the many
// direct config readers stay profile-agnostic. An unknown selection fails loud
// with the list of known profiles.
func loadRuntimeConfig(pathArgs ...runtimePaths) (runtimeConfig, error) {
	paths, err := resolveRuntimePaths(pathArgs...)
	if err != nil {
		return runtimeConfig{}, err
	}

	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		return runtimeConfig{}, err
	}
	setActiveServerProfile(name)
	cfg, ok := doc.flatProfile(name)
	if !ok || cfg.RelayBaseURL == "" {
		if name != defaultServerProfileName {
			return runtimeConfig{}, fmt.Errorf("server profile %q is not set up yet. Run: ha-nova pair --server %s --relay-url http://<ha-host>:8791", name, name)
		}
		return runtimeConfig{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	return cfg, nil
}

func loadConfig(pathArgs ...runtimePaths) (config, error) {
	return loadRuntimeConfig(pathArgs...)
}

func resolveRuntimePaths(pathArgs ...runtimePaths) (runtimePaths, error) {
	if len(pathArgs) > 0 {
		return pathArgs[0], nil
	}
	return detectPaths()
}

// saveConfig writes cfg into the SELECTED server profile of the on-disk
// document, preserving sibling profiles and unknown top-level fields, and
// mirrors the default profile into the legacy flat fields (downgrade floor).
func saveConfig(paths runtimePaths, cfg runtimeConfig) error {
	cfg.SchemaVersion = configSchemaVersion
	return saveProfileConfig(paths, cfg)
}
