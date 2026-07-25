package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloudIngressFunctionalResponseAcceptsRelayProof(t *testing.T) {
	deviceCredential := validCredential(24)
	for name, status := range map[string]int{
		"proven success":      http.StatusOK,
		"proven client error": http.StatusBadRequest,
	} {
		t.Run(name, func(t *testing.T) {
			client := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Header: http.Header{
							relayVersionHeader: []string{"1.2.3"},
						},
						Body: io.NopCloser(strings.NewReader(
							`{"ok":false,"error":{"code":"RELAY_RESULT"}}`,
						)),
					}, nil
				}),
			)
			response, err := client.Do(
				context.Background(),
				CloudEndpointWS,
				deviceCredential,
				[]byte(`{"type":"ping"}`),
			)
			if err != nil {
				t.Fatalf("proven functional response error = %v", err)
			}
			defer response.Body.Close()
			if !response.ReachedRelay ||
				response.RelayVersion != "1.2.3" ||
				response.StatusCode != status {
				t.Fatalf("proven functional response = %+v", response)
			}
		})
	}
}

func TestCloudIngressFunctionalDispatchFailuresAreOutcomeUnknown(t *testing.T) {
	deviceCredential := validCredential(22)
	for name, transport := range map[string]http.RoundTripper{
		"network result lost": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("ingress_session=must-not-leak")
		}),
		"server failure": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					`{"secret":"must-not-leak"}`,
				)),
			}, nil
		}),
	} {
		t.Run(name, func(t *testing.T) {
			client := newProtocolTestCloudIngressClient(t, transport)
			_, err := client.Do(
				context.Background(),
				CloudEndpointWS,
				deviceCredential,
				[]byte(`{"type":"config/area_registry/create"}`),
			)
			if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) ||
				strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("functional dispatch error = %v", err)
			}
		})
	}

	setup := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	}))
	_, err := setup.Do(
		context.Background(),
		CloudEndpointRelayInfo,
		"",
		nil,
	)
	if !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("setup dispatch error = %v", err)
	}
}

func TestCloudIngressDeviceLifecycleUsesExpectedRelayInstance(t *testing.T) {
	deviceCredential := validCredential(3)
	requests := 0
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"relay_instance_id":"relay-1"}` ||
			request.Header.Get("Authorization") != "Bearer "+deviceCredential {
			t.Fatalf("device lifecycle request body=%s auth=%q", body, request.Header.Get("Authorization"))
		}
		responseBody := `{"ok":true,"data":{"device_id":"device-1","bound":true,"changed":false}}`
		if strings.HasSuffix(request.URL.Path, CloudPathDeviceActivate) {
			responseBody = `{"ok":true,"data":{"device_id":"device-1","activated":true,"bound":true,"changed":true}}`
		} else if strings.HasSuffix(request.URL.Path, CloudPathDeviceRevoke) {
			responseBody = `{"ok":true,"data":{"device_id":"` +
				deviceIDOf(deviceCredential) +
				`","revoked":true,"changed":true}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}, nil
	}))
	bound, err := client.BindDevice(context.Background(), deviceCredential, "relay-1")
	if err != nil || !bound.Bound || bound.DeviceID != "device-1" {
		t.Fatalf("BindDevice = %+v, %v", bound, err)
	}
	activated, err := client.ActivateDevice(context.Background(), deviceCredential, "relay-1")
	if err != nil || !activated.Activated || !activated.Bound {
		t.Fatalf("ActivateDevice = %+v, %v", activated, err)
	}
	revoked, err := client.RevokeDevice(context.Background(), deviceCredential, "relay-1")
	if err != nil ||
		!revoked.Revoked ||
		revoked.DeviceID != deviceIDOf(deviceCredential) {
		t.Fatalf("RevokeDevice = %+v, %v", revoked, err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestCloudIngressDeviceRevokeRetriesExactRequestAfterLostOutcome(t *testing.T) {
	deviceCredential := validCredential(25)
	var bodies []string
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		bodies = append(bodies, string(body))
		if request.URL.Path !=
			"/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"+
				CloudPathDeviceRevoke {
			t.Fatalf("revoke path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer "+deviceCredential {
			t.Fatalf("revoke authorization = %q", request.Header.Get("Authorization"))
		}
		if len(bodies) == 1 {
			return nil, errors.New("response lost after dispatch")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":true,"data":{"device_id":"` +
					deviceIDOf(deviceCredential) +
					`","revoked":true,"changed":false}}`,
			)),
		}, nil
	}))
	result, err := client.RevokeDevice(
		context.Background(),
		deviceCredential,
		"relay-1",
	)
	if err != nil || !result.Revoked || result.Changed {
		t.Fatalf("RevokeDevice retry = %+v, %v", result, err)
	}
	if len(bodies) != 2 ||
		bodies[0] != `{"relay_instance_id":"relay-1"}` ||
		bodies[1] != bodies[0] {
		t.Fatalf("revoke retry bodies = %#v", bodies)
	}
}

func TestCloudIngressDeviceRevokeDoesNotRetryDefinitiveClientError(t *testing.T) {
	requests := 0
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":false}`)),
		}, nil
	}))
	_, err := client.RevokeDevice(
		context.Background(),
		validCredential(26),
		"relay-1",
	)
	if !IsCloudErrorCode(err, CloudErrDeviceRejected) {
		t.Fatalf("revoke client error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("revoke requests = %d, want 1", requests)
	}
}

func TestCloudIngressDeviceLifecycleClassifiesInstanceMismatch(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":false,"error":{"code":"RELAY_INSTANCE_MISMATCH","message":"sensitive"}}`,
			)),
		}, nil
	}))
	_, err := client.BindDevice(context.Background(), validCredential(4), "relay-1")
	if !IsCloudErrorCode(err, CloudErrRelayInstance) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("instance mismatch error = %v", err)
	}
}
