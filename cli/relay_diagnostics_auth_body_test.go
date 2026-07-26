package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type diagnosticAuthBody struct {
	read bool
}

func (b *diagnosticAuthBody) Read([]byte) (int, error) {
	b.read = true
	return 0, errors.New("body must not be read before auth classification")
}

func (*diagnosticAuthBody) Close() error {
	return nil
}

func TestRelayHealthClassifiesAuthBeforeReadingBody(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
	} {
		body := &diagnosticAuthBody{}
		client := diagnosticAuthClient(status, body)
		_, err := fetchRelayHealthWithContext(
			context.Background(),
			client,
			"http://relay.test",
			"token",
		)
		if err == nil ||
			!strings.Contains(err.Error(), http.StatusText(status)) &&
				!strings.Contains(err.Error(), "HTTP ") {
			t.Fatalf("status=%d error=%v", status, err)
		}
		if body.read {
			t.Fatalf("status=%d read auth body", status)
		}
	}
}

func TestRelayWSPingClassifiesAuthBeforeReadingBody(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
	} {
		body := &diagnosticAuthBody{}
		client := diagnosticAuthClient(status, body)
		response, err := probeRelayWSPingWithContext(
			context.Background(),
			client,
			"http://relay.test",
			"token",
		)
		if err != nil ||
			!relayWSPingIssueIsRelayAuth(response) {
			t.Fatalf(
				"status=%d response=%+v error=%v",
				status,
				response,
				err,
			)
		}
		if body.read {
			t.Fatalf("status=%d read auth body", status)
		}
	}
}

func TestRelayDiagnosticsRejectOversizedNonAuthBodies(t *testing.T) {
	oversized := strings.Repeat(
		"x",
		maxRelayDiagnosticResponseBytes+1,
	)
	healthClient := diagnosticAuthClient(
		http.StatusOK,
		io.NopCloser(strings.NewReader(oversized)),
	)
	if _, err := fetchRelayHealthWithContext(
		context.Background(),
		healthClient,
		"http://relay.test",
		"token",
	); err == nil {
		t.Fatal("health accepted oversized body")
	}

	wsClient := diagnosticAuthClient(
		http.StatusBadGateway,
		io.NopCloser(strings.NewReader(oversized)),
	)
	if _, err := probeRelayWSPingWithContext(
		context.Background(),
		wsClient,
		"http://relay.test",
		"token",
	); err == nil {
		t.Fatal("WS ping accepted oversized body")
	}
}

func diagnosticAuthClient(
	status int,
	body io.ReadCloser,
) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       body,
					Header:     make(http.Header),
				}, nil
			},
		),
	}
}
