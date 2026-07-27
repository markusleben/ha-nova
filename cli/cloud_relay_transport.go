package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const cloudRelayVirtualBaseURL = "https://relay.home-assistant-cloud.invalid"

type cloudRelayRoundTripper struct {
	ingress    *CloudIngressClient
	credential string
}

func newCloudRelayTransport(
	ingress *CloudIngressClient,
	deviceCredential string,
) (relayTransportSelection, error) {
	if ingress == nil || parseDeviceCredential(deviceCredential) == nil {
		return relayTransportSelection{}, newCloudError(
			CloudErrInvalidInput,
			"initialize Cloud Relay transport",
			nil,
		)
	}
	client := &http.Client{
		Transport: &cloudRelayRoundTripper{
			ingress:    ingress,
			credential: deviceCredential,
		},
		Timeout: 2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return relayTransportSelection{
		BaseURL:    cloudRelayVirtualBaseURL,
		Client:     client,
		Credential: deviceCredential,
		DeviceMode: true,
		Via:        relayViaCloud,
	}, nil
}

func (transport *cloudRelayRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	if transport == nil || transport.ingress == nil || request == nil ||
		request.URL == nil || request.URL.Scheme != "https" ||
		request.URL.Host != "relay.home-assistant-cloud.invalid" ||
		request.URL.RawQuery != "" || request.URL.Fragment != "" ||
		request.URL.RawPath != "" || request.URL.Opaque != "" ||
		request.URL.ForceQuery || request.URL.User != nil {
		return nil, newCloudError(CloudErrInvalidInput, "dispatch Cloud Relay request", nil)
	}
	endpoint, ok := cloudFunctionalEndpoint(request.Method, request.URL.Path)
	if !ok {
		return nil, newCloudError(CloudErrInvalidInput, "dispatch Cloud Relay request", nil)
	}
	authorization := request.Header.Values("Authorization")
	if len(authorization) != 1 ||
		authorization[0] != "Bearer "+transport.credential {
		return nil, newCloudError(CloudErrInvalidInput, "authorize Cloud Relay request", nil)
	}
	body, err := readCloudRelayRequest(request)
	if err != nil {
		return nil, err
	}
	ingressResponse, err := transport.ingress.Do(
		request.Context(),
		endpoint,
		transport.credential,
		body,
	)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header, 2)
	if ingressResponse.ContentType != "" {
		headers.Set("Content-Type", ingressResponse.ContentType)
	}
	if ingressResponse.RelayVersion != "" {
		headers.Set(relayVersionHeader, ingressResponse.RelayVersion)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", ingressResponse.StatusCode, http.StatusText(ingressResponse.StatusCode)),
		StatusCode:    ingressResponse.StatusCode,
		Header:        headers,
		Body:          ingressResponse.Body,
		ContentLength: -1,
		Request:       request,
	}, nil
}

func readCloudRelayRequest(request *http.Request) ([]byte, error) {
	if request.Body == nil || request.Body == http.NoBody {
		return nil, nil
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, cloudIngressMaxRequestBytes+1))
	if err != nil {
		return nil, newCloudError(CloudErrNetwork, "read Cloud Relay request", err)
	}
	if int64(len(body)) > cloudIngressMaxRequestBytes {
		return nil, newCloudError(CloudErrInvalidInput, "read Cloud Relay request", nil)
	}
	return body, nil
}

func cloudFunctionalEndpoint(method, path string) (CloudIngressEndpoint, bool) {
	method = strings.ToUpper(method)
	switch {
	case method == http.MethodGet && path == "/health":
		return CloudEndpointHealth, true
	case method == http.MethodPost && path == "/ws":
		return CloudEndpointWS, true
	case method == http.MethodPost && path == "/core":
		return CloudEndpointCore, true
	case method == http.MethodPost && path == "/files":
		return CloudEndpointFiles, true
	case method == http.MethodPost && path == "/backups":
		return CloudEndpointBackups, true
	default:
		return "", false
	}
}
