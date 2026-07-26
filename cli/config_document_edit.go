package main

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func validateSupportedConfigDocument(doc *configDocument) error {
	if err := validateSupportedConfigSchema(doc); err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	if err := validateClientInstallID(doc.meta.ClientInstallID); err != nil {
		return fmt.Errorf("invalid install identity in config.json: %w", err)
	}
	return nil
}

func validateSupportedConfigSchema(doc *configDocument) error {
	if doc == nil {
		return nil
	}
	if doc.meta.SchemaVersion > configSchemaVersion {
		return fmt.Errorf(
			"config schema_version %d is newer than this HA NOVA build supports (%d); update HA NOVA before using it",
			doc.meta.SchemaVersion,
			configSchemaVersion,
		)
	}
	return nil
}

// Document-level edit primitives for the `ha-nova server` profile-management
// commands (default/rename/remove). saveProfileConfig covers "write ONE
// profile's fields"; these cover structural edits of the servers map itself,
// with the same guarantees: siblings and unknown top-level fields preserved,
// legacy mirror refreshed from the LITERAL default profile.

// documentServersCopy returns a mutable copy of the servers map, migrating a
// v1 flat document on the way (the flat fields ARE the default profile).
func documentServersCopy(doc *configDocument) (map[string]json.RawMessage, error) {
	servers := make(map[string]json.RawMessage, len(doc.servers)+1)
	for name, raw := range doc.servers {
		servers[name] = raw
	}
	if doc.servers == nil && doc.flat != (serverProfileConfig{}) {
		raw, err := json.Marshal(doc.flat)
		if err != nil {
			return nil, err
		}
		servers[defaultServerProfileName] = raw
	}
	return servers, nil
}

// writeServersDocument writes the document back with the given servers map
// and default_server, preserving unknown top-level fields and refreshing the
// legacy mirror from the LITERAL default profile — never from default_server
// (config_profiles.go explains why). With no literal default profile there is
// no mirror: the flat fields are removed and an old binary honestly reports
// "not set up yet".
func writeServersDocument(paths runtimePaths, doc *configDocument, servers map[string]json.RawMessage, defaultName string) error {
	if err := validateSupportedConfigDocument(doc); err != nil {
		return err
	}
	if err := normalizeServerProfilesV3(servers); err != nil {
		return err
	}
	top := make(map[string]json.RawMessage, len(doc.top)+4)
	for key, value := range doc.top {
		top[key] = value
	}
	for _, key := range serverProfileFieldKeys {
		delete(top, key)
	}
	for _, key := range legacyMirrorFieldKeys {
		delete(top, key)
	}
	if raw, ok := servers[defaultServerProfileName]; ok {
		mirrorMap, err := legacyMirrorMap(raw)
		if err != nil {
			return err
		}
		for key, value := range mirrorMap {
			top[key] = value
		}
	}
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	top["servers"] = serversRaw
	top["schema_version"] = json.RawMessage(strconv.Itoa(configSchemaVersion))
	defaultRaw, err := json.Marshal(defaultName)
	if err != nil {
		return err
	}
	top["default_server"] = defaultRaw
	return writeJSONFile(paths.ConfigFile, top, 0o600)
}
