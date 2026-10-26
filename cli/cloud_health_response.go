package main

import (
	"bytes"
	"encoding/json"
	"errors"
)

// errRelayHealthIdentityMismatch: the App answered as another Relay than the
// saved one, usually a reinstalled App. The error code stays HA_PROTOCOL.
var errRelayHealthIdentityMismatch = errors.New("the Relay instance id differs from the saved one")

// validateCloudRelayHealthIdentity keeps the security-sensitive response
// envelope strict while allowing the Relay to add health metadata fields.
// Only relay_instance_id participates in the trust decision.
func validateCloudRelayHealthIdentity(
	body []byte,
	expectedRelayInstanceID string,
) error {
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return newCloudError(
			CloudErrHAProtocol,
			"decode Relay health response",
			err,
		)
	}
	if err := ensureJSONEOF(decoder); err != nil ||
		!envelope.OK ||
		len(envelope.Data) == 0 {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Relay health response envelope",
			err,
		)
	}
	var health struct {
		RelayInstanceID string `json:"relay_instance_id"`
	}
	if err := json.Unmarshal(envelope.Data, &health); err != nil {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Relay health identity",
			err,
		)
	}
	if health.RelayInstanceID != expectedRelayInstanceID {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Relay health identity",
			errRelayHealthIdentityMismatch,
		)
	}
	return nil
}
