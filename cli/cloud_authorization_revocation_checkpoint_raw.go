package main

import (
	"encoding/json"
)

func validateKnownAuthorizationRevocationCheckpointShape(
	raw json.RawMessage,
) error {
	var checkpoint cloudAuthorizationRevocationCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return err
	}
	return validateCloudAuthorizationRevocationCheckpoint(&checkpoint)
}
