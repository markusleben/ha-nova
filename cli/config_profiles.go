package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// Config schema v2 (multi-server): config.json carries a `servers` map of named
// profiles plus install-wide fields (`schema_version`, `default_server`,
// `client_install_id`). v1 flat configs migrate transparently: on load the flat
// fields ARE the default profile; the first save un-flattens them into
// `servers.default`. Every save also mirrors the default profile back into the
// legacy flat top-level fields, so an older binary keeps working against the
// default profile (downgrade floor).

// serverProfileConfig is the per-server field set — runtimeConfig minus the
// install-wide fields (schema_version, client_install_id). JSON tags match the
// v1 flat keys, so the same struct serves profile entries and the legacy mirror.
type serverProfileConfig struct {
	HAHost               string `json:"ha_host"`
	HAURL                string `json:"ha_url"`
	RelayBaseURL         string `json:"relay_base_url"`
	RelayTokenFile       string `json:"relay_token_file,omitempty"`
	RelaySecureBaseURL   string `json:"relay_secure_base_url,omitempty"`
	RelaySpkiPin         string `json:"relay_spki_pin,omitempty"`
	PendingSecureBaseURL string `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string `json:"pending_spki_pin,omitempty"`
}

// serverProfileFieldKeys are the per-server JSON keys — the fields the legacy
// mirror rewrites at the top level on every save.
var serverProfileFieldKeys = []string{
	"ha_host", "ha_url", "relay_base_url", "relay_token_file",
	"relay_secure_base_url", "relay_spki_pin", "pending_secure_base_url", "pending_spki_pin",
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
}

func loadConfigDocument(path string) (*configDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseConfigDocument(data)
}

func parseConfigDocument(data []byte) (*configDocument, error) {
	doc := &configDocument{}
	if err := json.Unmarshal(data, &doc.top); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &doc.meta); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &doc.flat); err != nil {
		return nil, err
	}
	if raw, ok := doc.top["servers"]; ok {
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
			if err := json.Unmarshal(raw, &parsed); err != nil {
				return runtimeConfig{}, false
			}
			fields = parsed
		case name == d.defaultServerName():
			// Hand-edited v2 without a default entry: the legacy mirror still
			// carries the default profile's data.
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
		RelaySecureBaseURL:   fields.RelaySecureBaseURL,
		RelaySpkiPin:         fields.RelaySpkiPin,
		PendingSecureBaseURL: fields.PendingSecureBaseURL,
		PendingSpkiPin:       fields.PendingSpkiPin,
	}, true
}

func serverProfileFromRuntime(cfg runtimeConfig) serverProfileConfig {
	return serverProfileConfig{
		HAHost:               cfg.HAHost,
		HAURL:                cfg.HAURL,
		RelayBaseURL:         cfg.RelayBaseURL,
		RelayTokenFile:       cfg.RelayTokenFile,
		RelaySecureBaseURL:   cfg.RelaySecureBaseURL,
		RelaySpkiPin:         cfg.RelaySpkiPin,
		PendingSecureBaseURL: cfg.PendingSecureBaseURL,
		PendingSpkiPin:       cfg.PendingSpkiPin,
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
	if cfg, ok := doc.flatProfile(doc.defaultServerName()); ok {
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
		RelaySecureBaseURL:   doc.flat.RelaySecureBaseURL,
		RelaySpkiPin:         doc.flat.RelaySpkiPin,
		PendingSecureBaseURL: doc.flat.PendingSecureBaseURL,
		PendingSpkiPin:       doc.flat.PendingSpkiPin,
	}
	return cfg, nil
}

// loadConfigDocumentOrEmpty backs the save path. A missing or unreadable
// config starts fresh — matching the pre-profile saveConfig, which always
// overwrote the file so a corrupted config stayed repairable via setup.
func loadConfigDocumentOrEmpty(path string) *configDocument {
	doc, err := loadConfigDocument(path)
	if err != nil {
		return &configDocument{top: map[string]json.RawMessage{}}
	}
	if doc.top == nil {
		doc.top = map[string]json.RawMessage{}
	}
	return doc
}

// saveTargetProfileName resolves which profile a save writes to (flag > env >
// configured default) and name-validates profiles that would be created.
func saveTargetProfileName(doc *configDocument) (string, error) {
	name, _ := requestedServerSelection()
	if name == "" {
		name = doc.defaultServerName()
	}
	if name != defaultServerProfileName && !doc.hasProfile(name) {
		if err := validateServerProfileName(name); err != nil {
			return "", err
		}
	}
	return name, nil
}

// withProfile builds the on-disk v2 document with cfg stored in the named
// profile: sibling profiles and unknown top-level fields are preserved, a v1
// flat document is migrated to the servers map, and the default profile is
// mirrored into the legacy flat fields (downgrade floor for older binaries).
func (d *configDocument) withProfile(name string, cfg runtimeConfig) (map[string]json.RawMessage, error) {
	top := make(map[string]json.RawMessage, len(d.top)+4)
	for key, value := range d.top {
		top[key] = value
	}
	servers := make(map[string]json.RawMessage, len(d.servers)+1)
	for key, value := range d.servers {
		servers[key] = value
	}
	if d.servers == nil && d.flat != (serverProfileConfig{}) {
		// v1 → v2 migration: the existing flat fields become the default
		// profile, so a save into another profile never destroys that server.
		raw, err := json.Marshal(d.flat)
		if err != nil {
			return nil, err
		}
		servers[defaultServerProfileName] = raw
	}
	profileRaw, err := json.Marshal(serverProfileFromRuntime(cfg))
	if err != nil {
		return nil, err
	}
	servers[name] = profileRaw

	defaultName := d.defaultServerName()
	if _, ok := servers[defaultName]; !ok {
		// The first profile ever saved becomes the default.
		defaultName = name
	}

	// Legacy mirror: the default profile's fields, rewritten on every save.
	var mirror serverProfileConfig
	if name == defaultName {
		mirror = serverProfileFromRuntime(cfg)
	} else if raw, ok := servers[defaultName]; ok {
		if err := json.Unmarshal(raw, &mirror); err != nil {
			return nil, err
		}
	} else {
		mirror = d.flat
	}
	for _, key := range serverProfileFieldKeys {
		delete(top, key)
	}
	mirrorRaw, err := json.Marshal(mirror)
	if err != nil {
		return nil, err
	}
	var mirrorMap map[string]json.RawMessage
	if err := json.Unmarshal(mirrorRaw, &mirrorMap); err != nil {
		return nil, err
	}
	for key, value := range mirrorMap {
		top[key] = value
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

	installID := cfg.ClientInstallID
	if installID == "" {
		installID = d.meta.ClientInstallID
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

// saveProfileConfig writes cfg into the selected profile of the on-disk
// document. All 8 read-modify-write config sites go through here (via
// saveConfig), so none of them can flatten the servers map away.
func saveProfileConfig(paths runtimePaths, cfg runtimeConfig) error {
	doc := loadConfigDocumentOrEmpty(paths.ConfigFile)
	name, err := saveTargetProfileName(doc)
	if err != nil {
		return err
	}
	top, err := doc.withProfile(name, cfg)
	if err != nil {
		return err
	}
	return writeJSONFile(paths.ConfigFile, top, 0o600)
}

// selectedServerProfileStatus reports the active profile name and how many
// profiles the config defines (best-effort; 1 when unreadable). Doctor uses it
// to name the checked profile on multi-server installs.
func selectedServerProfileStatus(paths runtimePaths) (string, int) {
	count := 1
	if doc, err := loadConfigDocument(paths.ConfigFile); err == nil {
		count = len(doc.profileNames())
	}
	return activeServerProfile(), count
}
