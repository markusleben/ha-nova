package main

import (
	"errors"
	"fmt"
	"os"
)

type runtimeConfig struct {
	SchemaVersion  int    `json:"schema_version"`
	HAHost         string `json:"ha_host"`
	HAURL          string `json:"ha_url"`
	RelayBaseURL   string `json:"relay_base_url"`
	RelayTokenFile string `json:"relay_token_file,omitempty"`
	// Stable profile identity. Unlike the user-facing profile name, this value
	// survives rename operations and binds profile-scoped Cloud credentials.
	ProfileID       string                  `json:"profile_id,omitempty"`
	RelayInstanceID string                  `json:"relay_instance_id,omitempty"`
	RoutePolicy     routePolicy             `json:"route_policy,omitempty"`
	Cloud           *cloudLifecycleMetadata `json:"cloud,omitempty"`
	// Stable, non-secret identifier for this OS-user installation. All AI clients
	// on the same machine share one device credential keyed by this id; the relay
	// records it so re-pairing replaces the same install's credential. Install-
	// wide: server profiles share it (see config_profiles.go), but each profile
	// pairs under its own derived id (pairingClientInstallID).
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
	PendingSecureBaseURL string                   `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string                   `json:"pending_spki_pin,omitempty"`
	ServerRemoval        *serverRemovalCheckpoint `json:"server_removal,omitempty"`
	// The demo house's public relay token (`ha-nova try`); set only on the
	// config loaded from demo.json, never saved to config.json.
	demoToken string
	// The NOVA App's internal device credential (`ha-nova serve
	// --relay-file`); never saved to config.json.
	bridgeCredential string
}

type config = runtimeConfig

// loadRuntimeConfig returns the flat runtimeConfig of the SELECTED server
// profile (--server flag > HA_NOVA_SERVER env > default_server), so the many
// direct config readers stay profile-agnostic. An unknown selection fails loud
// with the list of known profiles.
func loadRuntimeConfig(pathArgs ...runtimePaths) (runtimeConfig, error) {
	if bridgeRelay != nil {
		return *bridgeRelay, nil
	}
	paths, err := resolveRuntimePaths(pathArgs...)
	if err != nil {
		return runtimeConfig{}, err
	}
	if name, _ := requestedServerSelection(); name == demoProfileName && !realDemoProfileExists(paths) {
		cfg, on, err := loadDemoProfile(paths)
		if !on {
			return runtimeConfig{}, errors.New("The demo house is not connected. Run: ha-nova try")
		}
		setActiveDemoProfile()
		return cfg, err
	}
	notSetUp := errors.New("HA NOVA is not set up yet. Run: ha-nova setup")
	if demoProfileOn(paths) {
		notSetUp = errors.New("Only the demo house is connected. For it, add --server demo to every ha-nova command that reaches Home Assistant, but not to apply (it follows its plan). To connect your own home, run: ha-nova setup")
	}

	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return runtimeConfig{}, notSetUp
		}
		return runtimeConfig{}, fmt.Errorf(
			"cannot read HA NOVA server configuration %s: %w; restore or repair the file before retrying",
			paths.ConfigFile,
			err,
		)
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return runtimeConfig{}, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid server profile identities: %w", err)
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		if pick, _ := requestedServerSelection(); pick == "" && len(doc.servers) == 0 && errors.Is(err, errUnknownServerProfile) && demoProfileOn(paths) {
			return runtimeConfig{}, notSetUp
		}
		return runtimeConfig{}, err
	}
	setActiveServerProfile(name)
	cfg, ok := doc.flatProfile(name)
	if !ok {
		if name != defaultServerProfileName {
			return runtimeConfig{}, fmt.Errorf("server profile %q is not set up yet. Run: ha-nova pair --server %s --relay-url http://<ha-host>:8791", name, name)
		}
		return runtimeConfig{}, notSetUp
	}
	if err := rejectPendingServerRemoval(name, cfg); err != nil {
		return runtimeConfig{}, err
	}
	if err := validateLoadedRuntimeConfig(&cfg); err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid server profile %q: %w", name, err)
	}
	localReady := cfg.RelayBaseURL != ""
	cloudOnlyReady := effectiveRoutePolicy(cfg.RoutePolicy) == routePolicyCloud &&
		cfg.Cloud.functional()
	if !localReady && !cloudOnlyReady {
		if name != defaultServerProfileName {
			return runtimeConfig{}, fmt.Errorf("server profile %q is not set up yet. Run: ha-nova pair --server %s --relay-url http://<ha-host>:8791", name, name)
		}
		return runtimeConfig{}, notSetUp
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
// mirrors only the default profile's legacy local fields (downgrade floor).
func saveConfig(paths runtimePaths, cfg runtimeConfig) error {
	cfg.SchemaVersion = configSchemaVersion
	return saveProfileConfig(paths, cfg)
}
