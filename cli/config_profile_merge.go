package main

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func normalizeServerProfilesV3(servers map[string]json.RawMessage) error {
	seenProfileIDs := make(map[string]string, len(servers))
	for name, raw := range servers {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("invalid server profile %q: null", name)
		}
		var fields serverProfileConfig
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("invalid server profile %q: %w", name, err)
		}
		if err := ensureServerProfileFields(&fields); err != nil {
			return fmt.Errorf("invalid server profile %q: %w", name, err)
		}
		if previous, exists := seenProfileIDs[fields.ProfileID]; exists {
			return fmt.Errorf("server profiles %q and %q share profile_id %q", previous, name, fields.ProfileID)
		}
		seenProfileIDs[fields.ProfileID] = name
		merged, err := mergeServerProfileRaw(raw, fields)
		if err != nil {
			return fmt.Errorf("normalize server profile %q: %w", name, err)
		}
		servers[name] = merged
	}
	return nil
}

func validateUniqueServerProfileIDs(servers map[string]json.RawMessage) error {
	seen := make(map[string]string, len(servers))
	for name, raw := range servers {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("invalid server profile %q: null", name)
		}
		var fields serverProfileConfig
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("invalid server profile %q: %w", name, err)
		}
		if previous, exists := seen[fields.ProfileID]; exists {
			return fmt.Errorf("server profiles %q and %q share profile_id %q", previous, name, fields.ProfileID)
		}
		seen[fields.ProfileID] = name
	}
	return nil
}

func validateExistingServerProfileIDs(servers map[string]json.RawMessage) error {
	seen := make(map[string]string, len(servers))
	for name, raw := range servers {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("invalid server profile %q: null", name)
		}
		var fields serverProfileConfig
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("invalid server profile %q: %w", name, err)
		}
		if fields.ProfileID == "" {
			continue
		}
		if err := validateProfileID(fields.ProfileID); err != nil {
			return fmt.Errorf("invalid server profile %q: %w", name, err)
		}
		if previous, exists := seen[fields.ProfileID]; exists {
			return fmt.Errorf("server profiles %q and %q share profile_id %q", previous, name, fields.ProfileID)
		}
		seen[fields.ProfileID] = name
	}
	return nil
}

func mergeRuntimeProfile(existing json.RawMessage, cfg runtimeConfig) (json.RawMessage, error) {
	var previous serverProfileConfig
	if len(existing) > 0 {
		if bytes.Equal(bytes.TrimSpace(existing), []byte("null")) {
			return nil, fmt.Errorf("invalid existing server profile: null")
		}
		if err := json.Unmarshal(existing, &previous); err != nil {
			return nil, err
		}
	}
	fields := serverProfileFromRuntime(cfg)
	switch {
	case previous.ProfileID != "" && fields.ProfileID == "":
		fields.ProfileID = previous.ProfileID
	case previous.ProfileID != "" && fields.ProfileID != previous.ProfileID:
		return nil, fmt.Errorf("profile_id is immutable: existing %q, attempted %q", previous.ProfileID, fields.ProfileID)
	}
	if err := ensureServerProfileFields(&fields); err != nil {
		return nil, err
	}
	return mergeServerProfileRaw(existing, fields)
}

func ensureServerProfileFields(fields *serverProfileConfig) error {
	cfg := runtimeConfig{ProfileID: fields.ProfileID}
	if err := ensureProfileID(&cfg); err != nil {
		return err
	}
	fields.ProfileID = cfg.ProfileID
	if fields.RelayInstanceID != "" && !validIdentifier(fields.RelayInstanceID, 256) {
		return fmt.Errorf("invalid relay_instance_id")
	}
	fields.RoutePolicy = effectiveRoutePolicy(fields.RoutePolicy)
	if _, err := parseRoutePolicy(string(fields.RoutePolicy)); err != nil {
		return err
	}
	if err := normalizeCloudLifecycle(&fields.Cloud); err != nil {
		return err
	}
	cfg = runtimeConfig{
		RelayBaseURL:       fields.RelayBaseURL,
		ProfileID:          fields.ProfileID,
		RelayInstanceID:    fields.RelayInstanceID,
		RoutePolicy:        fields.RoutePolicy,
		Cloud:              fields.Cloud,
		RelaySecureBaseURL: fields.RelaySecureBaseURL,
		RelaySpkiPin:       fields.RelaySpkiPin,
	}
	return validateLoadedRuntimeConfig(&cfg)
}

func mergeServerProfileRaw(existing json.RawMessage, fields serverProfileConfig) (json.RawMessage, error) {
	rawMap := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &rawMap); err != nil {
			return nil, err
		}
	}
	existingCloud := rawMap["cloud"]
	for _, key := range serverProfileFieldKeys {
		delete(rawMap, key)
	}
	knownRaw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(knownRaw, &known); err != nil {
		return nil, err
	}
	for key, value := range known {
		if key == "cloud" {
			merged, err := mergeCloudRaw(existingCloud, value)
			if err != nil {
				return nil, err
			}
			rawMap[key] = merged
			continue
		}
		rawMap[key] = value
	}
	if _, hasKnownCloud := known["cloud"]; !hasKnownCloud {
		if preserved, ok, err := preserveUnknownCloudRaw(existingCloud); err != nil {
			return nil, err
		} else if ok {
			rawMap["cloud"] = preserved
		}
	}
	return json.Marshal(rawMap)
}

var cloudLifecycleFieldKeys = []string{
	"state",
	"current",
	"pending",
	"device_activation_started",
	"device_activation_device_id",
	"device_revocation_completed",
	"recovery_hold",
}
var cloudConnectionFieldKeys = []string{
	"origin", "canonical_origin", "oauth_client_id", "credential_generation", "ha_user_id",
}

func mergeCloudRaw(existing, known json.RawMessage) (json.RawMessage, error) {
	merged, err := mergeKnownJSONFields(existing, known, cloudLifecycleFieldKeys)
	if err != nil {
		return nil, err
	}
	var existingMap, knownMap map[string]json.RawMessage
	if err := json.Unmarshal(existing, &existingMap); err != nil && len(existing) > 0 {
		return nil, err
	}
	if err := json.Unmarshal(known, &knownMap); err != nil {
		return nil, err
	}
	var mergedMap map[string]json.RawMessage
	if err := json.Unmarshal(merged, &mergedMap); err != nil {
		return nil, err
	}
	for _, slot := range []string{"current", "pending"} {
		knownSlot, ok := knownMap[slot]
		if !ok {
			continue
		}
		existingSlot := existingMap[slot]
		if !sameJSONStringField(existingSlot, knownSlot, "credential_generation") {
			if slot == "current" &&
				sameJSONStringField(existingMap["pending"], knownSlot, "credential_generation") {
				existingSlot = existingMap["pending"]
			} else {
				existingSlot = nil
			}
		}
		nested, err := mergeKnownJSONFields(existingSlot, knownSlot, cloudConnectionFieldKeys)
		if err != nil {
			return nil, err
		}
		mergedMap[slot] = nested
	}
	return json.Marshal(mergedMap)
}

func sameJSONStringField(left, right json.RawMessage, field string) bool {
	var leftFields, rightFields map[string]json.RawMessage
	if json.Unmarshal(left, &leftFields) != nil || json.Unmarshal(right, &rightFields) != nil {
		return false
	}
	var leftValue, rightValue string
	if json.Unmarshal(leftFields[field], &leftValue) != nil || json.Unmarshal(rightFields[field], &rightValue) != nil {
		return false
	}
	return leftValue != "" && leftValue == rightValue
}

func preserveUnknownCloudRaw(existing json.RawMessage) (json.RawMessage, bool, error) {
	if len(existing) == 0 {
		return nil, false, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(existing, &fields); err != nil {
		return nil, false, err
	}
	for _, key := range cloudLifecycleFieldKeys {
		delete(fields, key)
	}
	if len(fields) == 0 {
		return nil, false, nil
	}
	raw, err := json.Marshal(fields)
	return raw, err == nil, err
}

func mergeKnownJSONFields(existing, known json.RawMessage, keys []string) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &merged); err != nil {
			return nil, err
		}
	}
	for _, key := range keys {
		delete(merged, key)
	}
	var replacements map[string]json.RawMessage
	if err := json.Unmarshal(known, &replacements); err != nil {
		return nil, err
	}
	for key, value := range replacements {
		merged[key] = value
	}
	return json.Marshal(merged)
}

func legacyMirrorMap(profileRaw json.RawMessage) (map[string]json.RawMessage, error) {
	var profile serverProfileConfig
	if err := json.Unmarshal(profileRaw, &profile); err != nil {
		return nil, err
	}
	legacy := legacyServerProfileConfig{
		HAHost:               profile.HAHost,
		HAURL:                profile.HAURL,
		RelayBaseURL:         profile.RelayBaseURL,
		RelayTokenFile:       profile.RelayTokenFile,
		RelaySecureBaseURL:   profile.RelaySecureBaseURL,
		RelaySpkiPin:         profile.RelaySpkiPin,
		PendingSecureBaseURL: profile.PendingSecureBaseURL,
		PendingSpkiPin:       profile.PendingSpkiPin,
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}
