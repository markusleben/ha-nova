package main

import (
	"encoding/json"
	"strconv"
)

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
	top := make(map[string]json.RawMessage, len(doc.top)+4)
	for key, value := range doc.top {
		top[key] = value
	}
	for _, key := range serverProfileFieldKeys {
		delete(top, key)
	}
	if raw, ok := servers[defaultServerProfileName]; ok {
		var mirror serverProfileConfig
		if err := json.Unmarshal(raw, &mirror); err != nil {
			return err
		}
		mirrorRaw, err := json.Marshal(mirror)
		if err != nil {
			return err
		}
		var mirrorMap map[string]json.RawMessage
		if err := json.Unmarshal(mirrorRaw, &mirrorMap); err != nil {
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
