package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type cloudRelayTransportFunc func(*http.Request) (*http.Response, error)

func (fn cloudRelayTransportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCloudRelayTransportCarriesOnlyIngressSessionAndDeviceCredential(t *testing.T) {
	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEF"
	const ingressSession = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	outer := &http.Client{Transport: cloudRelayTransportFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://example.ui.nabu.casa"+ingressRoot+"/ws" {
			t.Fatalf("outer URL = %q", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatalf("device Authorization = %q", request.Header.Get("Authorization"))
		}
		cookie, err := request.Cookie("ingress_session")
		if err != nil || cookie.Value != ingressSession {
			t.Fatalf("ingress cookie = %#v, %v", cookie, err)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != `{"type":"ping"}` {
			t.Fatalf("outer body = %q, %v", body, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":            []string{"application/json"},
				"X-Ha-Nova-Relay-Version": []string{"0.8.0"},
			},
			Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":{"type":"pong"}}`)),
		}, nil
	})}
	ingress, err := NewCloudIngressClient(
		"https://example.ui.nabu.casa",
		ingressRoot,
		ingressSession,
		outer,
	)
	if err != nil {
		t.Fatalf("NewCloudIngressClient() error = %v", err)
	}
	selection, err := newCloudRelayTransport(ingress, credential)
	if err != nil {
		t.Fatalf("newCloudRelayTransport() error = %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		selection.BaseURL+"/ws",
		bytes.NewBufferString(`{"type":"ping"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := selection.Client.Do(request)
	if err != nil {
		t.Fatalf("Cloud Relay request error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		response.Header.Get(relayVersionHeader) != "0.8.0" {
		t.Fatalf("response = %d, version %q", response.StatusCode, response.Header.Get(relayVersionHeader))
	}
}

func TestCloudRelayTransportRejectsUnknownRouteBeforeIngress(t *testing.T) {
	var calls atomic.Int32
	ingress := testCloudIngressClient(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, nil
	})
	selection, err := newCloudRelayTransport(
		ingress,
		"hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, selection.BaseURL+"/unknown", nil)
	request.Header.Set("Authorization", "Bearer "+selection.Credential)
	if _, err := selection.Client.Do(request); err == nil {
		t.Fatal("unknown Cloud Relay route was accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unknown route reached the ingress transport")
	}
}

func TestCloudRelayOutcomeUnknownHidesVirtualRoutingOrigin(t *testing.T) {
	message := relayRequestOutcomeUnknownMessage(
		cloudRelayVirtualBaseURL,
		&url.Error{
			Op:  "Post",
			URL: cloudRelayVirtualBaseURL + "/ws",
			Err: newCloudError(
				CloudErrNetwork,
				"send Cloud ingress request",
				nil,
			),
		},
	)
	if !strings.Contains(message, "OUTCOME_UNKNOWN") ||
		!strings.Contains(message, "Home Assistant Cloud") ||
		strings.Contains(message, cloudRelayVirtualBaseURL) {
		t.Fatalf("Cloud outcome message = %q", message)
	}
}

func testCloudIngressClient(
	t *testing.T,
	roundTrip cloudRelayTransportFunc,
) *CloudIngressClient {
	t.Helper()
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEF"
	const ingressSession = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	client, err := NewCloudIngressClient(
		"https://example.ui.nabu.casa",
		ingressRoot,
		ingressSession,
		&http.Client{Transport: roundTrip},
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
