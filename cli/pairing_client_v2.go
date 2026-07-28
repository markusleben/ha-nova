package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"

	"github.com/bytemare/opaque"
)

type cloudProvisionedCredential struct {
	Credential      string `json:"credential"`
	DeviceID        string `json:"device_id"`
	RelayInstanceID string `json:"relay_instance_id"`
}

type cloudPairingV2Info struct {
	RelayVersion    string `json:"relay_version"`
	RelayInstanceID string `json:"relay_instance_id"`
	ProtocolVersion string `json:"protocol_version"`
	Available       bool   `json:"available"`
}

// pairDeviceV2 performs the same OPAQUE + AEAD exchange as local v1, but every
// message stays inside an authenticated Supervisor Ingress session. The
// returned credential remains pending until the caller activates it.
func pairDeviceV2(
	ctx context.Context,
	client *CloudIngressClient,
	code string,
	metadata deviceMetadata,
	expectedRelayInstanceID string,
) (*cloudProvisionedCredential, error) {
	if client == nil || !validPairingCode(code) ||
		!validIdentifier(expectedRelayInstanceID, 256) ||
		!validCloudPairingMetadata(metadata) {
		return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud device pairing", nil)
	}
	var info cloudPairingV2Info
	if err := cloudPairingCall(ctx, client, CloudEndpointPairInfo, nil, &info); err != nil {
		return nil, err
	}
	if info.ProtocolVersion != "v2" || !info.Available ||
		info.RelayInstanceID != expectedRelayInstanceID ||
		!validIdentifier(info.RelayVersion, 128) {
		return nil, newCloudError(CloudErrRelayInstance, "verify Cloud pairing instance", nil)
	}
	conf := opaqueClientConfig()
	opaqueClient, err := conf.Client()
	if err != nil {
		return nil, fmt.Errorf("opaque client: %w", err)
	}
	ke1, err := opaqueClient.GenerateKE1([]byte(code))
	if err != nil {
		return nil, fmt.Errorf("opaque KE1: %w", err)
	}

	var start struct {
		HandshakeID string `json:"handshake_id"`
		KE2         string `json:"ke2"`
	}
	if err := cloudPairingCall(
		ctx,
		client,
		CloudEndpointPairStart,
		map[string]any{"ke1": pairB64.EncodeToString(ke1.Serialize())},
		&start,
	); err != nil {
		return nil, err
	}
	handshakeID, err := pairB64.DecodeString(start.HandshakeID)
	if err != nil || len(handshakeID) != 16 {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud pairing handshake", err)
	}
	ke2Bytes, err := pairB64.DecodeString(start.KE2)
	if err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud pairing response", err)
	}
	deserializer, err := conf.Deserializer()
	if err != nil {
		return nil, fmt.Errorf("opaque deserializer: %w", err)
	}
	ke2, err := deserializer.KE2(ke2Bytes)
	if err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud OPAQUE KE2", err)
	}
	options := &opaque.ClientOptions{
		KSFParameters: []uint64{3, 65536, 4},
		KSFSalt:       make([]byte, 16),
		KSFLength:     64,
	}
	ke3, sessionKey, _, err := opaqueClient.GenerateKE3(
		ke2,
		[]byte(pairingClientID),
		[]byte(pairingServerID),
		options,
	)
	if err != nil {
		return nil, &CloudError{
			Code:  CloudErrPairingRejected,
			Op:    "authenticate Cloud pairing code",
			cause: errPairingCodeRejected,
		}
	}

	clientToServer := derivePairKey(sessionKey, handshakeID, "c2s")
	serverToClient := derivePairKey(sessionKey, handshakeID, "s2c")
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, newCloudError(CloudErrInvalidInput, "encode Cloud pairing metadata", err)
	}
	encryptedMetadata := pairB64.EncodeToString(
		pairSeal(clientToServer, handshakeID, "c2s", metadataJSON),
	)

	var finish struct {
		Response string `json:"response"`
	}
	if err := cloudPairingFinishCall(
		ctx,
		client,
		map[string]any{
			"handshake_id": start.HandshakeID,
			"ke3":          pairB64.EncodeToString(ke3.Serialize()),
			"metadata":     encryptedMetadata,
		},
		&finish,
	); err != nil {
		return nil, err
	}
	frame, err := pairB64.DecodeString(finish.Response)
	if err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud pairing credential", err)
	}
	plaintext, ok := pairOpen(serverToClient, handshakeID, "s2c", frame)
	if !ok {
		return nil, newCloudError(CloudErrHAProtocol, "decrypt Cloud pairing credential", nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var provisioned cloudProvisionedCredential
	if err := decoder.Decode(&provisioned); err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud pairing credential", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, newCloudError(CloudErrHAProtocol, "decode Cloud pairing credential", err)
	}
	parsed := parseDeviceCredential(provisioned.Credential)
	if parsed == nil || provisioned.DeviceID != parsed.deviceID ||
		provisioned.RelayInstanceID != expectedRelayInstanceID {
		return nil, newCloudError(CloudErrRelayInstance, "validate Cloud pairing credential", nil)
	}
	return &provisioned, nil
}

func cloudPairingCall(
	ctx context.Context,
	client *CloudIngressClient,
	endpoint CloudIngressEndpoint,
	body any,
	result any,
) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return newCloudError(CloudErrInvalidInput, "encode Cloud pairing request", err)
		}
	}
	return cloudPairingCallEncoded(ctx, client, endpoint, encoded, result)
}

// cloudPairingFinishCall keeps the exact serialized finish request stable
// across bounded replays after an ambiguous transport, body-read, or 5xx
// result. Proven 3xx/4xx and protocol failures remain single-shot.
func cloudPairingFinishCall(
	ctx context.Context,
	client *CloudIngressClient,
	body any,
	result any,
) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return newCloudError(CloudErrInvalidInput, "encode Cloud pairing request", err)
	}
	err = retryPairingFinish(func() (bool, error) {
		var wroteRequest atomic.Bool
		trace := &httptrace.ClientTrace{
			WroteRequest: func(info httptrace.WroteRequestInfo) {
				if info.Err == nil {
					wroteRequest.Store(true)
				}
			},
		}
		attemptCtx := httptrace.WithClientTrace(ctx, trace)
		err := cloudPairingCallEncoded(
			attemptCtx,
			client,
			CloudEndpointPairFinish,
			encoded,
			result,
		)
		return cloudPairingFinishAttemptResult(ctx, wroteRequest.Load(), err)
	})
	if errors.Is(err, errPairingOutcomeUnknown) {
		return newCloudError(
			CloudErrOutcomeUnknown,
			"finish Cloud pairing",
			err,
		)
	}
	return err
}

func cloudPairingFinishAttemptResult(
	ctx context.Context,
	wroteRequest bool,
	err error,
) (bool, error) {
	ambiguous := cloudPairingFinishAmbiguous(wroteRequest, err)
	if ambiguous && ctx.Err() != nil {
		return false, fmt.Errorf("%w: %v", errPairingOutcomeUnknown, err)
	}
	return ambiguous, err
}

func cloudPairingFinishAmbiguous(wroteRequest bool, err error) bool {
	if err == nil {
		return false
	}
	var cloudErr *CloudError
	if !errors.As(err, &cloudErr) {
		return false
	}
	if cloudErr.Code == CloudErrIngressUnavailable {
		return false
	}
	if cloudErr.StatusCode != 0 {
		return cloudErr.StatusCode >= http.StatusInternalServerError &&
			cloudErr.StatusCode <= 599
	}
	return wroteRequest &&
		(cloudErr.Code == CloudErrNetwork || cloudErr.Code == CloudErrTimeout)
}

func cloudPairingCallEncoded(
	ctx context.Context,
	client *CloudIngressClient,
	endpoint CloudIngressEndpoint,
	encoded []byte,
	result any,
) error {
	response, err := client.Do(ctx, endpoint, "", encoded)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, readErr := readCloudResponse(response.Body, maxPairingBodyBytes, "read Cloud pairing response")
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusTooManyRequests {
			rateLimit := &relayPairingRateLimitError{
				retryAfterSeconds: pairingRetryAfterSeconds(response.RetryAfter),
			}
			return &CloudError{
				Code:       CloudErrPairingRateLimited,
				Op:         "perform Cloud pairing",
				StatusCode: response.StatusCode,
				Retryable:  true,
				cause:      rateLimit,
			}
		}
		legacy := pairingStatusError(response.StatusCode, raw, response.RetryAfter)
		code := CloudErrHAProtocol
		switch {
		case response.StatusCode == http.StatusUnauthorized:
			code = CloudErrPairingRejected
		case response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusConflict:
			code = CloudErrPairingInactive
		}
		return &CloudError{
			Code:       code,
			Op:         "perform Cloud pairing",
			StatusCode: response.StatusCode,
			Retryable:  response.StatusCode >= 500,
			cause:      legacy,
		}
	}
	if readErr != nil {
		return readErr
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	envelopeDecoder := json.NewDecoder(bytes.NewReader(raw))
	envelopeDecoder.DisallowUnknownFields()
	if err := envelopeDecoder.Decode(&envelope); err != nil || !envelope.OK ||
		len(envelope.Data) == 0 {
		return newCloudError(CloudErrHAProtocol, "decode Cloud pairing response", err)
	}
	if err := ensureJSONEOF(envelopeDecoder); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud pairing response", err)
	}
	resultDecoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	resultDecoder.DisallowUnknownFields()
	if err := resultDecoder.Decode(result); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud pairing response", err)
	}
	if err := ensureJSONEOF(resultDecoder); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud pairing response", err)
	}
	return nil
}

func validPairingCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validCloudPairingMetadata(metadata deviceMetadata) bool {
	return validIdentifier(metadata.Name, 64) &&
		validIdentifier(metadata.Platform, 64) &&
		validIdentifier(metadata.Client, 64) &&
		validIdentifier(metadata.ClientInstallID, 128)
}
