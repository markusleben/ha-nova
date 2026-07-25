package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type failingCloudIngressBody struct{}

func (failingCloudIngressBody) Read([]byte) (int, error) {
	return 0, errors.New("ingress_session=must-not-leak")
}

func (failingCloudIngressBody) Close() error {
	return nil
}

func newProtocolTestCloudIngressClient(t *testing.T, transport http.RoundTripper) *CloudIngressClient {
	t.Helper()
	client, err := NewCloudIngressClient(
		"https://unit.ui.nabu.casa",
		"/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789",
		strings.Repeat("a", 128),
		&http.Client{Transport: transport},
	)
	if err != nil {
		t.Fatalf("NewCloudIngressClient: %v", err)
	}
	return client
}

func TestCloudIngressClientSeparatesOuterCookieAndInnerDeviceBearer(t *testing.T) {
	deviceCredential := validCredential(0)
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://unit.ui.nabu.casa/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789/ws" ||
			request.Method != http.MethodPost {
			t.Fatalf("request target = %s %s", request.Method, request.URL.Redacted())
		}
		cookies := request.Cookies()
		if len(cookies) != 1 || cookies[0].Name != "ingress_session" ||
			cookies[0].Value != strings.Repeat("a", 128) {
			t.Fatalf("cookies = %+v", cookies)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+deviceCredential {
			t.Fatalf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"type":"ping"}` {
			t.Fatalf("body = %q", body)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				relayVersionHeader: []string{"1.2.3"},
				"Content-Type":     []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":{"pong":true}}`)),
		}, nil
	}))
	response, err := client.Do(
		context.Background(),
		CloudEndpointWS,
		deviceCredential,
		[]byte(`{"type":"ping"}`),
	)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	if !response.ReachedRelay || response.RelayVersion != "1.2.3" {
		t.Fatalf("response metadata = %+v", response)
	}
}

func TestCloudIngressRelayInfoUnwrapsAndValidatesExactWireEnvelope(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "" {
			t.Fatal("outer-only Relay info carried a device bearer")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(`{
				"ok":true,
				"data":{
					"protocol_version":"v1",
					"relay_instance_id":"relay-1",
					"relay_version":"1.2.3",
					"capabilities":{
						"device_user_binding":true,
						"pairing_v2":true,
						"functional_routes":["health","ws","core","files","backups"],
						"cleanup_routes":["device_revoke_self"]
					}
				}
			}`)),
		}, nil
	}))
	info, err := client.RelayInfo(context.Background())
	if err != nil {
		t.Fatalf("RelayInfo: %v", err)
	}
	if info.RelayInstanceID != "relay-1" ||
		!info.Capabilities.DeviceUserBinding || !info.Capabilities.PairingV2 {
		t.Fatalf("info = %+v", info)
	}
}

func TestCloudIngressRelayInfoRejectsMissingCapabilityAndUnknownWireField(t *testing.T) {
	for name, body := range map[string]string{
		"missing functional route": `{"ok":true,"data":{"protocol_version":"v1","relay_instance_id":"relay-1","relay_version":"1.2.3","capabilities":{"device_user_binding":true,"pairing_v2":true,"functional_routes":["health","ws","core","files"],"cleanup_routes":["device_revoke_self"]}}}`,
		"missing cleanup route":    `{"ok":true,"data":{"protocol_version":"v1","relay_instance_id":"relay-1","relay_version":"1.2.3","capabilities":{"device_user_binding":true,"pairing_v2":true,"functional_routes":["health","ws","core","files","backups"],"cleanup_routes":[]}}}`,
		"mixed enablement":         `{"ok":true,"data":{"protocol_version":"v1","relay_instance_id":"relay-1","relay_version":"1.2.3","capabilities":{"device_user_binding":true,"pairing_v2":false,"functional_routes":[],"cleanup_routes":["device_revoke_self"]}}}`,
		"unknown field":            `{"ok":true,"data":{"protocol_version":"v1","relay_instance_id":"relay-1","relay_version":"1.2.3","unexpected":true,"capabilities":{"device_user_binding":true,"pairing_v2":true,"functional_routes":["health","ws","core","files","backups"],"cleanup_routes":["device_revoke_self"]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
					Body:       io.NopCloser(strings.NewReader(body)),
				}, nil
			}))
			if _, err := client.RelayInfo(context.Background()); !IsCloudErrorCode(err, CloudErrHAProtocol) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCloudIngressRelayInfoAcceptsCleanupOnlyCapabilities(t *testing.T) {
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body: io.NopCloser(strings.NewReader(
				`{"ok":true,"data":{"protocol_version":"v1","relay_instance_id":"relay-1","relay_version":"1.2.3","capabilities":{"device_user_binding":false,"pairing_v2":false,"functional_routes":[],"cleanup_routes":["device_revoke_self"]}}}`,
			)),
		}, nil
	}))
	info, err := client.RelayInfo(context.Background())
	if err != nil {
		t.Fatalf("RelayInfo: %v", err)
	}
	if info.Capabilities.DeviceUserBinding ||
		info.Capabilities.PairingV2 ||
		len(info.Capabilities.FunctionalRoutes) != 0 ||
		info.RemoteEnabled() {
		t.Fatalf("cleanup-only info = %+v", info)
	}
}

func TestCloudIngressClientClassifiesOuterAndInnerAuthorizationFailures(t *testing.T) {
	deviceCredential := validCredential(1)
	for name, testCase := range map[string]struct {
		header http.Header
		want   CloudErrorCode
	}{
		"outer": {header: make(http.Header), want: CloudErrOuterSessionExpired},
		"inner": {header: http.Header{relayVersionHeader: []string{"1.2.3"}}, want: CloudErrDeviceRejected},
	} {
		t.Run(name, func(t *testing.T) {
			client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     testCase.header,
					Body:       io.NopCloser(strings.NewReader(`{"secret":"must not surface"}`)),
				}, nil
			}))
			_, err := client.Do(context.Background(), CloudEndpointHealth, deviceCredential, nil)
			if !IsCloudErrorCode(err, testCase.want) || strings.Contains(err.Error(), "must not surface") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCloudIngressClientRejectsRedirectAndBoundsStreamingResponse(t *testing.T) {
	deviceCredential := validCredential(2)
	redirectCalls := 0
	redirect := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirectCalls++
		if request.URL.Host == "evil.invalid" {
			t.Fatal("functional ingress redirect was followed")
		}
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"https://evil.invalid/steal"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}))
	if _, err := redirect.Do(
		context.Background(),
		CloudEndpointHealth,
		deviceCredential,
		nil,
	); !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("functional redirect error = %v", err)
	}
	if redirectCalls != 1 {
		t.Fatalf("functional redirect requests = %d, want 1", redirectCalls)
	}

	setupRedirect := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"https://evil.invalid/steal"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}))
	if _, err := setupRedirect.Do(
		context.Background(),
		CloudEndpointRelayInfo,
		"",
		nil,
	); !IsCloudErrorCode(err, CloudErrRedirectRejected) {
		t.Fatalf("setup redirect error = %v", err)
	}

	bounded := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body:       io.NopCloser(strings.NewReader("four")),
		}, nil
	}))
	bounded.maxResponse = 3
	response, err := bounded.Do(context.Background(), CloudEndpointHealth, deviceCredential, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) || string(data) != "fou" {
		t.Fatalf("bounded read data=%q err=%v", data, err)
	}

	failing := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body:       failingCloudIngressBody{},
		}, nil
	}))
	failedResponse, err := failing.Do(
		context.Background(),
		CloudEndpointHealth,
		deviceCredential,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer failedResponse.Body.Close()
	_, err = io.ReadAll(failedResponse.Body)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) ||
		strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("body read error = %v", err)
	}

	setupBounded := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{relayVersionHeader: []string{"1.2.3"}},
			Body:       io.NopCloser(strings.NewReader("four")),
		}, nil
	}))
	setupBounded.maxResponse = 3
	setupResponse, err := setupBounded.Do(
		context.Background(),
		CloudEndpointRelayInfo,
		"",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer setupResponse.Body.Close()
	_, err = io.ReadAll(setupResponse.Body)
	if !IsCloudErrorCode(err, CloudErrResponseTooLarge) {
		t.Fatalf("setup response cap error = %v", err)
	}
}

func TestCloudIngressFunctionalResponseRequiresRelayProof(t *testing.T) {
	deviceCredential := validCredential(23)
	for name, status := range map[string]int{
		"unproven success":      http.StatusOK,
		"unproven client error": http.StatusBadRequest,
	} {
		t.Run(name, func(t *testing.T) {
			client := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Header:     make(http.Header),
						Body: io.NopCloser(strings.NewReader(
							`{"ok":false,"error":{"code":"must-not-be-trusted"}}`,
						)),
					}, nil
				}),
			)
			_, err := client.Do(
				context.Background(),
				CloudEndpointWS,
				deviceCredential,
				[]byte(`{"type":"ping"}`),
			)
			if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) ||
				strings.Contains(err.Error(), "must-not-be-trusted") {
				t.Fatalf("unproven functional response error = %v", err)
			}
		})
	}

	duplicateProof := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					relayVersionHeader: []string{"1.2.3", "9.9.9"},
				},
				Body: io.NopCloser(strings.NewReader(
					`{"ok":true,"data":{"untrusted":true}}`,
				)),
			}, nil
		}),
	)
	if _, err := duplicateProof.Do(
		context.Background(),
		CloudEndpointHealth,
		deviceCredential,
		nil,
	); !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("duplicate Relay proof error = %v", err)
	}

	outerNotFound := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("not found")),
			}, nil
		}),
	)
	if _, err := outerNotFound.Do(
		context.Background(),
		CloudEndpointHealth,
		deviceCredential,
		nil,
	); !IsCloudErrorCode(err, CloudErrIngressUnavailable) {
		t.Fatalf("outer 404 error = %v", err)
	}
}
