package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// Config schema v3 adds immutable profile identities and transport policy while
// retaining the v2 servers map. v1/v2 profiles migrate locally on their next
// save: no Cloud route is inferred or contacted.

// serverProfileConfig is the per-server field set — runtimeConfig minus the
// install-wide fields (schema_version, client_install_id). JSON tags match the
// v1 flat keys, so the same struct serves profile entries and the legacy mirror.
type serverProfileConfig struct {
	HAHost               string                   `json:"ha_host"`
	HAURL                string                   `json:"ha_url"`
	RelayBaseURL         string                   `json:"relay_base_url"`
	RelayTokenFile       string                   `json:"relay_token_file,omitempty"`
	ProfileID            string                   `json:"profile_id,omitempty"`
	RelayInstanceID      string                   `json:"relay_instance_id,omitempty"`
	RoutePolicy          routePolicy              `json:"route_policy,omitempty"`
	Cloud                *cloudLifecycleMetadata  `json:"cloud,omitempty"`
	RelaySecureBaseURL   string                   `json:"relay_secure_base_url,omitempty"`
	RelaySpkiPin         string                   `json:"relay_spki_pin,omitempty"`
	PendingSecureBaseURL string                   `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string                   `json:"pending_spki_pin,omitempty"`
	ServerRemoval        *serverRemovalCheckpoint `json:"server_removal,omitempty"`
}

// serverProfileFieldKeys are replaced inside one profile while unknown sibling
// and per-profile fields stay untouched.
var serverProfileFieldKeys = []string{
	"ha_host", "ha_url", "relay_base_url", "relay_token_file",
	"profile_id", "relay_instance_id", "route_policy", "cloud",
	"relay_secure_base_url", "relay_spki_pin", "pending_secure_base_url", "pending_spki_pin",
	"server_removal",
}

// Only pre-v3 fields are mirrored at the top level. Cloud and stable profile
// identity must never leak into the downgrade shape read by older binaries.
var legacyMirrorFieldKeys = []string{
	"ha_host", "ha_url", "relay_base_url", "relay_token_file",
	"relay_secure_base_url", "relay_spki_pin", "pending_secure_base_url", "pending_spki_pin",
}

type legacyServerProfileConfig struct {
	HAHost               string `json:"ha_host"`
	HAURL                string `json:"ha_url"`
	RelayBaseURL         string `json:"relay_base_url"`
	RelayTokenFile       string `json:"relay_token_file,omitempty"`
	RelaySecureBaseURL   string `json:"relay_secure_base_url,omitempty"`
	RelaySpkiPin         string `json:"relay_spki_pin,omitempty"`
	PendingSecureBaseURL string `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string `json:"pending_spki_pin,omitempty"`
}

type configDocumentMeta struct {
	SchemaVersion   int    `json:"schema_version"`
	DefaultServer   string `json:"default_server"`
	ClientInstallID string `json:"client_install_id"`
}

// configDocument is the raw view of config.json: the typed install-wide fields,
// the parsed flat/mirror fields, and every profile kept as raw JSON so saves
// preserve sibling profiles byte-for-byte and unknown top-level fields intact.
type configDocument struct {
	top     map[string]json.RawMessage
	servers map[string]json.RawMessage // nil while the config is still flat (v1)
	meta    configDocumentMeta
	flat    serverProfileConfig // top-level flat fields (v1 shape / legacy mirror)
	source  []byte              // exact full-file generation read from disk
}

func loadConfigDocument(path string) (*configDocument, error) {
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(path),
	); err == nil {
		return nil, errors.New(
			"an interrupted configuration transaction must be recovered before reading config.json",
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseConfigDocument(data)
}

func parseConfigDocument(data []byte) (*configDocument, error) {
	doc := &configDocument{
		source: append([]byte(nil), data...),
	}
	if err := json.Unmarshal(data, &doc.top); err != nil {
		return nil, err
	}
	if doc.top == nil {
		return nil, fmt.Errorf("config document must be a JSON object")
	}
	if err := json.Unmarshal(data, &doc.meta); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &doc.flat); err != nil {
		return nil, err
	}
	if raw, ok := doc.top["servers"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("invalid servers map in config: null")
		}
		if err := json.Unmarshal(raw, &doc.servers); err != nil {
			return nil, fmt.Errorf("invalid servers map in config: %w", err)
		}
	}
	return doc, nil
}

func (d *configDocument) defaultServerName() string {
	if d.meta.DefaultServer != "" {
		return d.meta.DefaultServer
	}
	return defaultServerProfileName
}

func (d *configDocument) hasProfile(name string) bool {
	if d.servers == nil {
		return name == defaultServerProfileName
	}
	_, ok := d.servers[name]
	return ok
}

func (d *configDocument) profileNames() []string {
	if d.servers == nil {
		return []string{defaultServerProfileName}
	}
	names := make([]string, 0, len(d.servers))
	for name := range d.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// flatProfile returns the flat runtimeConfig view of one profile: its
// per-server fields plus the install-wide fields shared by all profiles.
func (d *configDocument) flatProfile(name string) (runtimeConfig, bool) {
	fields := d.flat
	if d.servers == nil {
		if name != defaultServerProfileName {
			return runtimeConfig{}, false
		}
	} else {
		raw, ok := d.servers[name]
		switch {
		case ok:
			var parsed serverProfileConfig
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return runtimeConfig{}, false
			}
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return runtimeConfig{}, false
			}
			fields = parsed
		case name == defaultServerProfileName:
			// Hand-edited v2 without a default entry: the legacy mirror (when
			// present) carries the LITERAL default profile's data.
		default:
			return runtimeConfig{}, false
		}
	}
	return runtimeConfig{
		SchemaVersion:        d.meta.SchemaVersion,
		ClientInstallID:      d.meta.ClientInstallID,
		HAHost:               fields.HAHost,
		HAURL:                fields.HAURL,
		RelayBaseURL:         fields.RelayBaseURL,
		RelayTokenFile:       fields.RelayTokenFile,
		ProfileID:            fields.ProfileID,
		RelayInstanceID:      fields.RelayInstanceID,
		RoutePolicy:          effectiveRoutePolicy(fields.RoutePolicy),
		Cloud:                fields.Cloud,
		RelaySecureBaseURL:   fields.RelaySecureBaseURL,
		RelaySpkiPin:         fields.RelaySpkiPin,
		PendingSecureBaseURL: fields.PendingSecureBaseURL,
		PendingSpkiPin:       fields.PendingSpkiPin,
		ServerRemoval:        fields.ServerRemoval,
	}, true
}

func serverProfileFromRuntime(cfg runtimeConfig) serverProfileConfig {
	return serverProfileConfig{
		HAHost:               cfg.HAHost,
		HAURL:                cfg.HAURL,
		RelayBaseURL:         cfg.RelayBaseURL,
		RelayTokenFile:       cfg.RelayTokenFile,
		ProfileID:            cfg.ProfileID,
		RelayInstanceID:      cfg.RelayInstanceID,
		RoutePolicy:          effectiveRoutePolicy(cfg.RoutePolicy),
		Cloud:                cfg.Cloud,
		RelaySecureBaseURL:   cfg.RelaySecureBaseURL,
		RelaySpkiPin:         cfg.RelaySpkiPin,
		PendingSecureBaseURL: cfg.PendingSecureBaseURL,
		PendingSpkiPin:       cfg.PendingSpkiPin,
		ServerRemoval:        cfg.ServerRemoval,
	}
}

// loadRawDefaultProfileConfig reads config.json WITHOUT the setup-completeness
// check of loadConfig, understanding both the v1 flat shape and the v2 servers
// map. It is deliberately independent of loadConfig and runtime selection
// (issue #200): relay-token storage must not depend on relay fields or on a
// valid profile selection, and the legacy token machinery is default-profile-
// only, so this always reads the DEFAULT profile.
func loadRawDefaultProfileConfig(path string) (runtimeConfig, error) {
	doc, err := loadConfigDocument(path)
	if err != nil {
		return runtimeConfig{}, err
	}
	// Always the LITERAL default profile — never default_server: the legacy
	// token (and its relay_token_file) belongs to the migrated v1 install even
	// when the user later points default_server at another profile.
	if cfg, ok := doc.flatProfile(defaultServerProfileName); ok {
		return cfg, nil
	}
	// No usable default entry: fall back to the flat/mirror fields.
	cfg := runtimeConfig{
		SchemaVersion:        doc.meta.SchemaVersion,
		ClientInstallID:      doc.meta.ClientInstallID,
		HAHost:               doc.flat.HAHost,
		HAURL:                doc.flat.HAURL,
		RelayBaseURL:         doc.flat.RelayBaseURL,
		RelayTokenFile:       doc.flat.RelayTokenFile,
		ProfileID:            doc.flat.ProfileID,
		RelayInstanceID:      doc.flat.RelayInstanceID,
		RoutePolicy:          effectiveRoutePolicy(doc.flat.RoutePolicy),
		Cloud:                doc.flat.Cloud,
		RelaySecureBaseURL:   doc.flat.RelaySecureBaseURL,
		RelaySpkiPin:         doc.flat.RelaySpkiPin,
		PendingSecureBaseURL: doc.flat.PendingSecureBaseURL,
		PendingSpkiPin:       doc.flat.PendingSpkiPin,
		ServerRemoval:        doc.flat.ServerRemoval,
	}
	return cfg, nil
}

// saveTargetProfileName resolves which profile a save writes to (flag > env >
// configured default) and name-validates profiles that would be created.
func saveTargetProfileName(doc *configDocument) (string, error) {
	name, _ := requestedServerSelection()
	if name == "" {
		name = doc.defaultServerName()
	}
	if err := validateServerProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

// withProfile builds the on-disk v3 document with cfg stored in the named
// profile. The first v3 save adds local-only profile identities to every v1/v2
// profile; later saves preserve every unknown per-profile field.
func (d *configDocument) withProfile(name string, cfg runtimeConfig) (map[string]json.RawMessage, error) {
	return d.withProfileDocument(name, cfg, true, false)
}

// withProfilePreservingSiblings is reserved for security cleanup. Revoking one
// profile's Cloud authorization must not be blocked by an unrelated sibling's
// invalid route/lifecycle fields, but profile identities still must parse,
// validate, and remain unique so the cleanup cannot target a shared account.
func (d *configDocument) withProfilePreservingSiblings(
	name string,
	cfg runtimeConfig,
) (map[string]json.RawMessage, error) {
	return d.withProfileDocument(name, cfg, false, false)
}

func (d *configDocument) withProfileDocument(
	name string,
	cfg runtimeConfig,
	normalizeSiblings bool,
	preserveInvalidInstallIdentity bool,
) (map[string]json.RawMessage, error) {
	if err := validateClientInstallID(cfg.ClientInstallID); err != nil &&
		(!preserveInvalidInstallIdentity ||
			cfg.ClientInstallID != d.meta.ClientInstallID) {
		return nil, err
	}
	top := make(map[string]json.RawMessage, len(d.top)+4)
	for key, value := range d.top {
		top[key] = value
	}
	servers, err := documentServersCopy(d)
	if err != nil {
		return nil, err
	}
	// Normalize siblings first, but merge the selected profile from its original
	// raw value. Setup may have just generated the selected profile's stable ID
	// in memory while migrating a v1/v2 document. Normalizing that same profile
	// first would generate a different random ID and then reject the intended
	// one as an immutable replacement.
	targetRaw := servers[name]
	if normalizeSiblings {
		delete(servers, name)
		if err := normalizeServerProfilesV3(servers); err != nil {
			return nil, err
		}
	}
	profileRaw, err := mergeRuntimeProfile(targetRaw, cfg)
	if err != nil {
		return nil, err
	}
	servers[name] = profileRaw
	if normalizeSiblings {
		if err := validateUniqueServerProfileIDs(servers); err != nil {
			return nil, err
		}
	} else if err := validateExistingServerProfileIDs(servers); err != nil {
		return nil, err
	}

	defaultName := d.defaultServerName()
	if _, ok := servers[defaultName]; !ok {
		// The first profile ever saved becomes the default.
		defaultName = name
	}

	// Legacy mirror: the LITERAL default profile's fields, rewritten on every
	// save — never default_server's. An old binary pairs the flat fields with
	// the machine-wide legacy token, which belongs to the migrated v1 install;
	// mirroring a named profile would send that token to another server. With
	// no literal default profile there is no mirror: the old binary honestly
	// reports "not set up yet".
	for _, key := range serverProfileFieldKeys {
		delete(top, key)
	}
	for _, key := range legacyMirrorFieldKeys {
		delete(top, key)
	}
	if raw, ok := servers[defaultServerProfileName]; ok {
		mirrorMap, err := legacyMirrorMap(raw)
		if err != nil {
			return nil, err
		}
		for key, value := range mirrorMap {
			top[key] = value
		}
	}

	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return nil, err
	}
	top["servers"] = serversRaw
	top["schema_version"] = json.RawMessage(strconv.Itoa(configSchemaVersion))
	defaultRaw, err := json.Marshal(defaultName)
	if err != nil {
		return nil, err
	}
	top["default_server"] = defaultRaw

	installID := d.meta.ClientInstallID
	if installID != "" &&
		cfg.ClientInstallID != "" &&
		cfg.ClientInstallID != installID {
		return nil, fmt.Errorf(
			"refusing to replace immutable client_install_id %q with %q",
			installID,
			cfg.ClientInstallID,
		)
	}
	if installID == "" {
		installID = cfg.ClientInstallID
	}
	if installID != "" {
		idRaw, err := json.Marshal(installID)
		if err != nil {
			return nil, err
		}
		top["client_install_id"] = idRaw
	} else {
		delete(top, "client_install_id")
	}
	return top, nil
}
