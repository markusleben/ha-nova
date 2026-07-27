package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type cloudReleaseStressRoundTripper struct {
	requests atomic.Int64
	failAt   int64
	relayID  string
}

func (transport *cloudReleaseStressRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	current := transport.requests.Add(1)
	if transport.failAt != 0 && current == transport.failAt {
		return nil, errors.New("synthetic transport failure")
	}
	if request.Method != http.MethodGet ||
		request.URL.String() != cloudRelayVirtualBaseURL+"/health" ||
		request.Header.Get("Authorization") != "Bearer test-device-token" ||
		request.Header.Get("Accept") != "application/json" {
		return nil, errors.New("unexpected stress request")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"ok":true,"data":{"relay_instance_id":"` +
				transport.relayID + `"}}`,
		)),
		Header:  make(http.Header),
		Request: request,
	}, nil
}

func TestCloudReleaseStressUsesExactCountAndOneResolvedTransport(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"client_install_id": "install-1234567890abcdef",
		"relay_base_url": "https://local.invalid",
		"profile_id": "profile-1234567890abcdef",
		"relay_instance_id": "relay-1234567890abcdef",
		"route_policy": "cloud",
		"cloud": {
			"state": "ready",
			"current": {
				"origin": "https://example.ui.nabu.casa",
				"canonical_origin": "https://example.ui.nabu.casa",
				"ha_user_id": "user-1234567890abcdef",
				"oauth_client_id": "http://127.0.0.1:49152/ha-nova",
				"credential_generation": "dddddddddddddddddddddddddddddddd"
			}
		}
	}`)
	deviceCredential := validCredential(30)
	var requests atomic.Int64
	ingress := newProtocolTestCloudIngressClient(
		t,
		cloudRuntimeRoundTripFunc(
			func(request *http.Request) (*http.Response, error) {
				requests.Add(1)
				if request.Method != http.MethodGet ||
					request.URL.Path !=
						"/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789/health" ||
					request.Header.Get("Authorization") !=
						"Bearer "+deviceCredential ||
					request.Header.Get("Accept") != "application/json" {
					return nil, errors.New("unexpected ingress stress request")
				}
				cookies := request.Cookies()
				if len(cookies) != 1 ||
					cookies[0].Name != "ingress_session" ||
					cookies[0].Value != strings.Repeat("a", 128) {
					return nil, errors.New("unexpected ingress session")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"ok":true,"data":{"relay_instance_id":"relay-1234567890abcdef"}}`,
					)),
					Header: http.Header{
						relayVersionHeader: []string{"1.2.3"},
					},
					Request: request,
				}, nil
			},
		),
	)
	selection, err := newCloudRelayTransport(
		ingress,
		deviceCredential,
	)
	if err != nil {
		t.Fatal(err)
	}
	var resolverCalls atomic.Int64
	previousResolver := resolveCloudRelayTransportForCLI
	resolveCloudRelayTransportForCLI = func(
		context.Context,
		runtimeConfig,
	) (relayTransportSelection, error) {
		resolverCalls.Add(1)
		return selection, nil
	}
	t.Cleanup(func() {
		resolveCloudRelayTransportForCLI = previousResolver
	})

	exit, output := captureCommandOutput(t, func() int {
		return runInternalCloudReleaseStress(paths, nil)
	})
	if exit != 0 {
		t.Fatalf("stress exit=%d output=%s", exit, output)
	}
	if resolverCalls.Load() != 1 {
		t.Fatalf("Cloud resolver calls=%d, want 1", resolverCalls.Load())
	}
	if requests.Load() != cloudReleaseStressRequestCount {
		t.Fatalf(
			"stress requests=%d, want %d",
			requests.Load(),
			cloudReleaseStressRequestCount,
		)
	}
	if !strings.Contains(
		output,
		"10000/10000 read-only requests through one process-local Ingress session",
	) {
		t.Fatalf("stress output=%s", output)
	}
}

func TestCloudReleaseStressStopsAtFirstFailure(
	t *testing.T,
) {
	cfg := runtimeConfig{
		RelayInstanceID: "relay-1234567890abcdef",
	}
	transport := &cloudReleaseStressRoundTripper{
		failAt:  3,
		relayID: cfg.RelayInstanceID,
	}
	err := runCloudReleaseStress(
		context.Background(),
		cfg,
		relayTransportSelection{
			BaseURL:    cloudRelayVirtualBaseURL,
			Client:     &http.Client{Transport: transport},
			Credential: "test-device-token",
			Via:        relayViaCloud,
		},
	)
	var failure *cloudReleaseStressFailure
	if !errors.As(err, &failure) || failure.request != 3 {
		t.Fatalf("stress error=%T %v", err, err)
	}
	if transport.requests.Load() != 3 {
		t.Fatalf("stress requests=%d, want 3", transport.requests.Load())
	}
}

func TestCloudReleaseStressRejectsWrongRelayIdentity(t *testing.T) {
	transport := &cloudReleaseStressRoundTripper{
		relayID: "relay-other-1234567890",
	}
	err := runCloudReleaseStressRequest(
		context.Background(),
		&http.Client{Transport: transport},
		cloudRelayVirtualBaseURL+"/health",
		"test-device-token",
		"relay-expected-1234567890",
	)
	if !IsCloudErrorCode(err, CloudErrHAProtocol) {
		t.Fatalf("wrong-identity error=%T %v", err, err)
	}
	if transport.requests.Load() != 1 {
		t.Fatalf("stress requests=%d, want 1", transport.requests.Load())
	}
}

func TestCloudReleaseStressRejectsUnsafeResponses(t *testing.T) {
	validBody := []byte(
		`{"ok":true,"data":{"relay_instance_id":"relay-expected-1234567890"}}`,
	)
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		wantCode   CloudErrorCode
	}{
		{
			name:       "redirect",
			statusCode: http.StatusFound,
			body:       validBody,
			wantCode:   CloudErrRedirectRejected,
		},
		{
			name:       "non-200",
			statusCode: http.StatusServiceUnavailable,
			body:       validBody,
			wantCode:   CloudErrHAProtocol,
		},
		{
			name:       "oversized",
			statusCode: http.StatusOK,
			body: bytes.Repeat(
				[]byte("x"),
				cloudLocalDiscoveryMaxBytes+1,
			),
			wantCode: CloudErrResponseTooLarge,
		},
		{
			name:       "invalid UTF-8",
			statusCode: http.StatusOK,
			body:       []byte{0xff},
			wantCode:   CloudErrHAProtocol,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{
				Transport: cloudRuntimeRoundTripFunc(
					func(request *http.Request) (*http.Response, error) {
						return &http.Response{
							StatusCode: test.statusCode,
							Body: io.NopCloser(
								bytes.NewReader(test.body),
							),
							Header:  make(http.Header),
							Request: request,
						}, nil
					},
				),
			}
			err := runCloudReleaseStressRequest(
				context.Background(),
				client,
				cloudRelayVirtualBaseURL+"/health",
				"test-device-token",
				"relay-expected-1234567890",
			)
			if !IsCloudErrorCode(err, test.wantCode) {
				t.Fatalf(
					"unsafe response error=%T %v, want %s",
					err,
					err,
					test.wantCode,
				)
			}
		})
	}
}
