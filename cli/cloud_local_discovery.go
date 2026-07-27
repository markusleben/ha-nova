package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const cloudLocalDiscoveryMaxBytes = 256 << 10

type cloudLocalDiscovery struct {
	Origin          CloudOrigin
	RelayInstanceID string
}

// discoverCloudFromLocalRelay keeps the existing-install flow URL-free. Both
// probes are authenticated, read-only Relay calls. Redirects are rejected so a
// local proxy cannot move either credential-bearing request to another origin.
func discoverCloudFromLocalRelay(ctx context.Context, cfg runtimeConfig) (cloudLocalDiscovery, error) {
	transport, err := resolveRelayTransport(
		ctx,
		resolveLocalRelayTransportForCLI,
		cfg,
	)
	if err != nil {
		return cloudLocalDiscovery{}, err
	}
	if transport.Via != relayViaLocal {
		return cloudLocalDiscovery{}, newCloudError(
			CloudErrIdentityMismatch,
			"select local Relay discovery transport",
			nil,
		)
	}
	client := cloudNoRedirectHTTPClient(transport.Client, 15*time.Second)

	var health struct {
		RelayInstanceID string `json:"relay_instance_id"`
	}
	if err := callLocalRelayJSON(
		ctx,
		client,
		transport.BaseURL,
		transport.Credential,
		http.MethodGet,
		"/health",
		nil,
		&health,
	); err != nil {
		return cloudLocalDiscovery{}, err
	}
	if !validIdentifier(health.RelayInstanceID, 256) {
		return cloudLocalDiscovery{}, newCloudError(
			CloudErrAppNotReady,
			"discover local NOVA Relay instance",
			nil,
		)
	}

	var status HACloudStatus
	if err := callLocalRelayJSON(
		ctx,
		client,
		transport.BaseURL,
		transport.Credential,
		http.MethodPost,
		"/ws",
		map[string]any{"type": "cloud/status"},
		&status,
	); err != nil {
		return cloudLocalDiscovery{}, err
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(status.RemoteDomain), "."))
	origin, err := ResolveCanonicalNabuOrigin(ctx, "https://"+domain, nil)
	if err != nil {
		return cloudLocalDiscovery{}, newCloudError(
			CloudErrCloudNotReady,
			"discover Home Assistant Cloud origin",
			err,
		)
	}
	if err := status.ValidateForOrigin(origin); err != nil {
		return cloudLocalDiscovery{}, err
	}
	return cloudLocalDiscovery{
		Origin:          origin,
		RelayInstanceID: health.RelayInstanceID,
	}, nil
}

func callLocalRelayJSON(
	ctx context.Context,
	client *http.Client,
	baseURL, credential, method, path string,
	body any,
	result any,
) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return newCloudError(CloudErrInvalidInput, "encode local Relay discovery request", err)
		}
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		strings.TrimRight(baseURL, "/")+path,
		bytes.NewReader(encoded),
	)
	if err != nil {
		return newCloudError(CloudErrInvalidInput, "build local Relay discovery request", err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return cloudRequestError("call local Relay discovery", err)
	}
	defer response.Body.Close()
	if isHTTPRedirect(response.StatusCode) {
		return newCloudHTTPError(
			CloudErrRedirectRejected,
			"call local Relay discovery",
			response.StatusCode,
			false,
		)
	}
	raw, err := readCloudResponse(
		response.Body,
		cloudLocalDiscoveryMaxBytes,
		"read local Relay discovery response",
	)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		code := CloudErrHAProtocol
		if response.StatusCode == http.StatusUnauthorized {
			code = CloudErrUnauthorized
		} else if response.StatusCode == http.StatusForbidden {
			code = CloudErrForbidden
		}
		return newCloudHTTPError(code, "call local Relay discovery", response.StatusCode, false)
	}
	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil ||
		ensureJSONEOF(decoder) != nil ||
		!envelope.OK ||
		len(envelope.Data) == 0 {
		return newCloudError(CloudErrHAProtocol, "decode local Relay discovery response", err)
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return newCloudError(
			CloudErrHAProtocol,
			fmt.Sprintf("decode local Relay %s response", path),
			err,
		)
	}
	return nil
}
