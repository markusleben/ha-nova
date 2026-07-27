package main

import "testing"

func TestValidateCloudRelayHealthIdentityAcceptsFullHealthPayload(t *testing.T) {
	body := []byte(`{
		"ok": true,
		"data": {
			"status": "ok",
			"ha_ws_connected": true,
			"ha_ws_disconnect_reason": null,
			"version": "1.2.3",
			"uptime_s": 42,
			"file_access": "allowlist",
			"snapshots": {"files": 2, "bytes": 128},
			"relay_instance_id": "relay-1",
			"future_health_field": true
		}
	}`)
	if err := validateCloudRelayHealthIdentity(body, "relay-1"); err != nil {
		t.Fatalf("validateCloudRelayHealthIdentity() error = %v", err)
	}
}

func TestValidateCloudRelayHealthIdentityRejectsEnvelopeExtension(t *testing.T) {
	body := []byte(`{
		"ok": true,
		"data": {"relay_instance_id": "relay-1"},
		"unexpected": true
	}`)
	err := validateCloudRelayHealthIdentity(body, "relay-1")
	if !IsCloudErrorCode(err, CloudErrHAProtocol) {
		t.Fatalf("error = %v, want HAProtocol", err)
	}
}

func TestValidateCloudRelayHealthIdentityRejectsWrongRelay(t *testing.T) {
	body := []byte(`{"ok":true,"data":{"relay_instance_id":"relay-2"}}`)
	err := validateCloudRelayHealthIdentity(body, "relay-1")
	if !IsCloudErrorCode(err, CloudErrHAProtocol) {
		t.Fatalf("error = %v, want HAProtocol", err)
	}
}
