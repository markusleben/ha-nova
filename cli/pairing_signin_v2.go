package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"
)

// Home Assistant sign-in pairing (docs/work/2026-10-16-ha-sign-in-pairing-spec.md):
// the owner's ingress session arms a one-time secret, and the existing pair-v2
// OPAQUE exchange runs with it through the same ingress session. The relay
// answers with an unbound local device plus its TLS pin, so the shared local
// tail (pending -> pinned-TLS activation -> promotion) takes over unchanged.

var (
	errSignInNotOwner         = errors.New("signed-in Home Assistant user is not the owner")
	errSignInRelayUnsupported = errors.New("NOVA Relay App does not support sign-in pairing")
	errSignInRelayNotReady    = errors.New("NOVA Relay secure port is not ready")
)

// The relay re-reads its secure port mapping on demand; one short retry
// usually finds it.
var signInArmRetryDelay = 2 * time.Second

// 32 bytes as unpadded base64url: the armed secret and the SPKI SHA-256 pin.
var base64URL32Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type signInArmResponse struct {
	PairingSecret   string `json:"pairing_secret"`
	ExpiresInS      int    `json:"expires_in_s"`
	RelayInstanceID string `json:"relay_instance_id"`
}

type signInProvisionedCredential struct {
	Credential      string `json:"credential"`
	DeviceID        string `json:"device_id"`
	RelayInstanceID string `json:"relay_instance_id"`
	SpkiPin         string `json:"spki_pin"`
	SecurePort      int    `json:"secure_port"`
}

func pairDeviceV2Armed(
	ctx context.Context,
	client *CloudIngressClient,
	metadata deviceMetadata,
) (*provisionedCredential, error) {
	armed, err := armSignInPairing(ctx, client, CloudEndpointPairArm)
	if errors.Is(err, errSignInRelayNotReady) {
		time.Sleep(signInArmRetryDelay)
		armed, err = armSignInPairing(ctx, client, CloudEndpointPairArm)
	}
	if err != nil {
		return nil, err
	}
	plaintext, err := pairV2Exchange(
		ctx,
		client,
		armed.PairingSecret,
		metadata,
		armed.RelayInstanceID,
	)
	if err != nil {
		return nil, err
	}
	var provisioned signInProvisionedCredential
	if err := decodeStrictJSON(plaintext, &provisioned); err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode sign-in pairing credential", err)
	}
	parsed := parseDeviceCredential(provisioned.Credential)
	if parsed == nil || provisioned.DeviceID != parsed.deviceID ||
		provisioned.RelayInstanceID != armed.RelayInstanceID ||
		!base64URL32Pattern.MatchString(provisioned.SpkiPin) ||
		provisioned.SecurePort < 1 || provisioned.SecurePort > 65535 {
		return nil, newCloudError(CloudErrRelayInstance, "validate sign-in pairing credential", nil)
	}
	return &provisionedCredential{
		Credential: provisioned.Credential,
		DeviceID:   provisioned.DeviceID,
		SpkiPin:    provisioned.SpkiPin,
		SecurePort: provisioned.SecurePort,
	}, nil
}

// pairDeviceV2LinkOwner pairs over Home Assistant Link without a code when the
// Link sign-in user is the owner (docs/work/2026-10-18-onboarding-0.27-spec.md
// §7.2): the Relay re-checks ownership with Home Assistant, arms a one-time
// secret for this ingress user, and answers with the same user-bound Cloud
// device as the owner code; the caller activates it over ingress as before.
// An App without /pair/v2/arm/link answers 404: errSignInRelayUnsupported.
func pairDeviceV2LinkOwner(
	ctx context.Context,
	client *CloudIngressClient,
	metadata deviceMetadata,
	expectedRelayInstanceID string,
) (*cloudProvisionedCredential, error) {
	armed, err := armSignInPairing(ctx, client, CloudEndpointPairArmLink)
	if err != nil {
		return nil, err
	}
	if armed.RelayInstanceID != expectedRelayInstanceID {
		return nil, newCloudError(CloudErrRelayInstance, "verify Link pairing instance", nil)
	}
	plaintext, err := pairV2Exchange(ctx, client, armed.PairingSecret, metadata, expectedRelayInstanceID)
	if err != nil {
		return nil, err
	}
	return decodeCloudProvisionedCredential(plaintext, expectedRelayInstanceID)
}

func armSignInPairing(
	ctx context.Context,
	client *CloudIngressClient,
	endpoint CloudIngressEndpoint,
) (signInArmResponse, error) {
	if client == nil {
		return signInArmResponse{}, newCloudError(CloudErrInvalidInput, "arm sign-in pairing", nil)
	}
	response, err := client.Do(ctx, endpoint, "", []byte("{}"))
	if err != nil {
		return signInArmResponse{}, err
	}
	defer response.Body.Close()
	raw, err := readCloudResponse(response.Body, maxPairingBodyBytes, "read sign-in pairing response")
	if err != nil {
		return signInArmResponse{}, err
	}
	switch {
	case response.StatusCode == http.StatusOK && response.ReachedRelay:
	case response.StatusCode == http.StatusNotFound && response.ReachedRelay:
		// An older relay, or a build without the ingress pairing surface.
		return signInArmResponse{}, errSignInRelayUnsupported
	case response.StatusCode == http.StatusForbidden && response.ReachedRelay &&
		relayPairingErrorCode(raw) == "OWNER_REQUIRED":
		return signInArmResponse{}, errSignInNotOwner
	case response.StatusCode == http.StatusServiceUnavailable && response.ReachedRelay &&
		relayPairingErrorCode(raw) == "PAIRING_UNAVAILABLE":
		return signInArmResponse{}, errSignInRelayNotReady
	default:
		return signInArmResponse{}, newCloudHTTPError(
			CloudErrHAProtocol,
			"arm sign-in pairing",
			response.StatusCode,
			response.StatusCode >= http.StatusInternalServerError,
		)
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	var armed signInArmResponse
	if err := decodeStrictJSON(raw, &envelope); err != nil || !envelope.OK ||
		decodeStrictJSON(envelope.Data, &armed) != nil ||
		!base64URL32Pattern.MatchString(armed.PairingSecret) ||
		armed.ExpiresInS < 1 || armed.ExpiresInS > 600 ||
		!validIdentifier(armed.RelayInstanceID, 256) {
		return signInArmResponse{}, newCloudError(CloudErrHAProtocol, "decode sign-in pairing response", err)
	}
	return armed, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}
