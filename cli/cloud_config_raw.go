package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func profileHasCloudLifecycleRaw(
	doc *configDocument,
	profileName string,
) (bool, error) {
	raw, err := cloudRecoveryProfileRaw(doc, profileName)
	if err != nil {
		return false, err
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil || profile == nil {
		return false, fmt.Errorf("invalid server profile %q", profileName)
	}
	rawCloud, exists := profile["cloud"]
	if !exists {
		return false, nil
	}
	trimmed := bytes.TrimSpace(rawCloud)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")), nil
}

func remainingCloudCleanupProfile(
	doc *configDocument,
) (profileName string, found bool, err error) {
	if doc == nil {
		return "", false, fmt.Errorf("missing configuration document")
	}
	if doc.servers != nil {
		if rawCloud, exists := doc.top["cloud"]; exists {
			trimmed := bytes.TrimSpace(rawCloud)
			if len(trimmed) > 0 &&
				!bytes.Equal(trimmed, []byte("null")) {
				return "", true, nil
			}
		}
	}
	for _, name := range doc.profileNames() {
		hasCloud, profileErr := profileHasCloudLifecycleRaw(doc, name)
		if profileErr != nil {
			return "", false, profileErr
		}
		if hasCloud {
			return name, true, nil
		}
	}
	return "", false, nil
}
