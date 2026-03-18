package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

type relayWSPingResponse struct {
	StatusCode int
	Body       []byte
}

func probeRelayWSPing(relayBaseURL, token string) (relayWSPingResponse, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/ws"
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(`{"type":"ping"}`)))
	if err != nil {
		return relayWSPingResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
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
	return resp.StatusCode == http.StatusOK
}

func relayWSPingIssueIsLLAT(resp relayWSPingResponse) bool {
	return resp.StatusCode == http.StatusBadGateway && strings.Contains(string(resp.Body), "LLAT is required")
}

func relayWSPingIssueIsRelayAuth(resp relayWSPingResponse) bool {
	return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
}
