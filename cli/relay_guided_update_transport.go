package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

// relayCoreRequest posts one request through the relay's /core proxy and
// returns the raw envelope body.
func relayCoreRequest(cfg config, token, method, path string, requestBody []byte) ([]byte, error) {
	base, client, credential, endpointErr := functionalEndpoint(cfg, token)
	if endpointErr != nil {
		return nil, endpointErr
	}
	return relayCoreRequestWithTransport(context.Background(), base, client, credential, method, path, requestBody)
}

func relayCoreRequestWithTransport(
	ctx context.Context,
	base string,
	client *http.Client,
	credential string,
	method string,
	path string,
	requestBody []byte,
) ([]byte, error) {
	payload := []byte(fmt.Sprintf(`{"method":%q,"path":%q`, method, path))
	if len(requestBody) > 0 {
		payload = append(payload, []byte(`,"body":`)...)
		payload = append(payload, requestBody...)
	}
	payload = append(payload, '}')
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/core", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readAllLimited(resp.Body, maxRelayResponseBytes)
}

func relayWSRequest(cfg config, token string, requestBody []byte) ([]byte, error) {
	base, client, credential, endpointErr := functionalEndpoint(cfg, token)
	if endpointErr != nil {
		return nil, endpointErr
	}
	return relayWSRequestWithTransport(context.Background(), base, client, credential, requestBody)
}

func relayWSRequestWithTransport(
	ctx context.Context,
	base string,
	client *http.Client,
	credential string,
	requestBody []byte,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/ws", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readAllLimited(resp.Body, maxRelayResponseBytes)
}
