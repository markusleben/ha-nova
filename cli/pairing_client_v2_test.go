package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloudPairingInfoUsesGETAndStrictEnvelope(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || !strings.HasSuffix(request.URL.Path, CloudPathPairInfo) {
			t.Fatalf("pairing info request = %s %s", request.Method, request.URL.Redacted())
		}
		body, _ := io.ReadAll(request.Body)
		if len(body) != 0 {
			t.Fatalf("GET pairing info body = %q", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":true,"data":{"relay_version":"1.2.3","relay_instance_id":"relay-1","protocol_version":"v2","available":true}}`,
			)),
		}, nil
	}))
	var info cloudPairingV2Info
	if err := cloudPairingCall(
		context.Background(),
		client,
		CloudEndpointPairInfo,
		nil,
		&info,
	); err != nil {
		t.Fatalf("cloudPairingCall: %v", err)
	}
	if info.ProtocolVersion != "v2" || info.RelayInstanceID != "relay-1" {
		t.Fatalf("info = %+v", info)
	}
}

func TestCloudPairingHonorsRetryAfterAndPreservesLegacyIdentity(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				relayVersionHeader: []string{"1.2.3"},
				"Retry-After":      []string{"275"},
			},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":false,"error":{"code":"PAIRING_RATE_LIMITED","message":"secret"}}`,
			)),
		}, nil
	}))
	var result any
	err := cloudPairingCall(
		context.Background(),
		client,
		CloudEndpointPairStart,
		map[string]any{"ke1": "abc"},
		&result,
	)
	if !IsCloudErrorCode(err, CloudErrPairingRateLimited) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("rate-limit error = %v", err)
	}
	var rateLimit *relayPairingRateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.retryAfterSeconds != 275 {
		t.Fatalf("legacy rate-limit identity = %+v", rateLimit)
	}
}

func TestCloudPairingReadsProvenRelayUnauthorizedResponse(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":false,"error":{"code":"PAIRING_FAILED","message":"generic"}}`,
			)),
		}, nil
	}))
	var result any
	err := cloudPairingCall(
		context.Background(),
		client,
		CloudEndpointPairFinish,
		map[string]any{
			"handshake_id": "abc",
			"ke3":          "def",
			"metadata":     "ghi",
		},
		&result,
	)
	if !IsCloudErrorCode(err, CloudErrPairingRejected) {
		t.Fatalf("proven Relay pairing rejection = %v", err)
	}
}

func TestCloudPairingRejectsUnknownSuccessFields(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":true,"unexpected":"value","data":{"handshake_id":"abc","ke2":"def"}}`,
			)),
		}, nil
	}))
	var result struct {
		HandshakeID string `json:"handshake_id"`
		KE2         string `json:"ke2"`
	}
	err := cloudPairingCall(
		context.Background(),
		client,
		CloudEndpointPairStart,
		map[string]any{"ke1": "abc"},
		&result,
	)
	if !IsCloudErrorCode(err, CloudErrHAProtocol) {
		t.Fatalf("unknown-field error = %v", err)
	}
}
