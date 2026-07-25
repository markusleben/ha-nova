package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

func deletePendingCloudDeviceCredentialWithContext(
	ctx context.Context,
) error {
	pending, exists, err := readPendingDeviceCredentialRecordWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil || !exists {
		return err
	}
	if pending.Source != pendingDeviceCredentialSourceCloud {
		return nil
	}
	if err := deletePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return fmt.Errorf(
			"remove pending Cloud device credential: %w",
			err,
		)
	}
	return nil
}

func prepareCloudRemovalDocument(
	paths runtimePaths,
	updated runtimeConfig,
) (map[string]json.RawMessage, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return nil, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return nil, err
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		return nil, err
	}
	if doc.servers != nil {
		if rawCloud, exists := doc.top["cloud"]; exists &&
			len(bytes.TrimSpace(rawCloud)) > 0 &&
			!bytes.Equal(
				bytes.TrimSpace(rawCloud),
				[]byte("null"),
			) {
			return nil, unknownCloudRemovalShape(name)
		}
	}
	var rawProfile json.RawMessage
	if doc.servers == nil {
		rawProfile, err = json.Marshal(doc.top)
	} else {
		rawProfile = doc.servers[name]
	}
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawProfile, &fields); err != nil ||
		fields == nil {
		return nil, fmt.Errorf("inspect selected server profile")
	}
	if rawCloud, exists := fields["cloud"]; exists &&
		len(bytes.TrimSpace(rawCloud)) > 0 &&
		!bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
		if err := validateKnownCloudRemovalShape(name, rawCloud); err != nil {
			return nil, err
		}
	}
	return doc.withProfilePreservingSiblings(name, updated)
}
