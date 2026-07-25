package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type relayWSPingResponse struct {
	StatusCode int
	Body       []byte
}

func probeRelayWSPing(relayBaseURL, token string) (relayWSPingResponse, error) {
	return probeRelayWSPingWith(httpClient, relayBaseURL, token)
}

func probeRelayWSPingWith(client *http.Client, relayBaseURL, token string) (relayWSPingResponse, error) {
	return probeRelayWSPingWithContext(
		context.Background(),
		client,
		relayBaseURL,
		token,
	)
}

func probeRelayWSPingWithContext(
	ctx context.Context,
	client *http.Client,
	relayBaseURL, token string,
) (relayWSPingResponse, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/ws"
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader([]byte(`{"type":"ping"}`)),
	)
	if err != nil {
		return relayWSPingResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return relayWSPingResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return relayWSPingResponse{}, err
	}
	return relayWSPingResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
	}, nil
}

func relayWSPingOK(resp relayWSPingResponse) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var envelope struct {
		OK bool `json:"ok"`
	}
	return json.Unmarshal(resp.Body, &envelope) == nil && envelope.OK
}

func relayWSPingIssueIsUpstreamAuth(resp relayWSPingResponse) bool {
	if resp.StatusCode != http.StatusBadGateway {
		return false
	}
	body := strings.ToLower(string(resp.Body))
	return strings.Contains(body, "llat is required") ||
		strings.Contains(body, "upstream access token") ||
		strings.Contains(body, "long-lived access token")
}

func relayWSPingIssueIsRelayAuth(resp relayWSPingResponse) bool {
	return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
}
