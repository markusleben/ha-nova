package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	CloudEndpointRelayInfo      CloudIngressEndpoint = "relay_info"
	CloudEndpointDeviceBind     CloudIngressEndpoint = "device_bind"
	CloudEndpointDeviceActivate CloudIngressEndpoint = "device_activate"
	CloudEndpointDeviceRevoke   CloudIngressEndpoint = "device_revoke"
	CloudEndpointPairInfo       CloudIngressEndpoint = "pair_info"
	CloudEndpointPairStart      CloudIngressEndpoint = "pair_start"
	CloudEndpointPairFinish     CloudIngressEndpoint = "pair_finish"
	CloudEndpointHealth         CloudIngressEndpoint = "health"
	CloudEndpointWS             CloudIngressEndpoint = "ws"
	CloudEndpointCore           CloudIngressEndpoint = "core"
	CloudEndpointFiles          CloudIngressEndpoint = "files"
	CloudEndpointBackups        CloudIngressEndpoint = "backups"
)

const (
	CloudPathRelayInfo      = "/cloud/v1/info"
	CloudPathDeviceBind     = "/cloud/v1/device/bind"
	CloudPathDeviceActivate = "/cloud/v1/device/activate"
	CloudPathDeviceRevoke   = "/cloud/v1/device/revoke-self"
	CloudPathPairInfo       = "/pair/v2/info"
	CloudPathPairStart      = "/pair/v2/start"
	CloudPathPairFinish     = "/pair/v2/finish"
)

const (
	cloudIngressMaxRequestBytes  = 1 << 20
	cloudIngressMaxResponseBytes = 256 << 20
)

type CloudIngressEndpoint string

type cloudIngressEndpointContract struct {
	method       string
	path         string
	deviceBearer bool
}

var cloudIngressEndpointContracts = map[CloudIngressEndpoint]cloudIngressEndpointContract{
	CloudEndpointRelayInfo:      {method: http.MethodGet, path: CloudPathRelayInfo},
	CloudEndpointDeviceBind:     {method: http.MethodPost, path: CloudPathDeviceBind, deviceBearer: true},
	CloudEndpointDeviceActivate: {method: http.MethodPost, path: CloudPathDeviceActivate, deviceBearer: true},
	CloudEndpointDeviceRevoke:   {method: http.MethodPost, path: CloudPathDeviceRevoke, deviceBearer: true},
	CloudEndpointPairInfo:       {method: http.MethodGet, path: CloudPathPairInfo},
	CloudEndpointPairStart:      {method: http.MethodPost, path: CloudPathPairStart},
	CloudEndpointPairFinish:     {method: http.MethodPost, path: CloudPathPairFinish},
	CloudEndpointHealth:         {method: http.MethodGet, path: "/health", deviceBearer: true},
	CloudEndpointWS:             {method: http.MethodPost, path: "/ws", deviceBearer: true},
	CloudEndpointCore:           {method: http.MethodPost, path: "/core", deviceBearer: true},
	CloudEndpointFiles:          {method: http.MethodPost, path: "/files", deviceBearer: true},
	CloudEndpointBackups:        {method: http.MethodPost, path: "/backups", deviceBearer: true},
}

type CloudIngressClient struct {
	origin      string
	ingressRoot string
	session     string
	http        *http.Client
	maxRequest  int64
	maxResponse int64
}

type CloudIngressResponse struct {
	StatusCode   int
	ContentType  string
	RelayVersion string
	RetryAfter   string
	ReachedRelay bool
	Body         io.ReadCloser
}

func NewCloudIngressClient(
	canonicalOrigin, ingressRoot, ingressSession string,
	httpClient *http.Client,
) (*CloudIngressClient, error) {
	origin, err := ParseCanonicalNabuOrigin(canonicalOrigin)
	if err != nil {
		return nil, err
	}
	if !supervisorIngressEntryPattern.MatchString(ingressRoot) ||
		!supervisorIngressSessionPattern.MatchString(ingressSession) {
		return nil, newCloudError(CloudErrInvalidInput, "initialize Cloud ingress client", nil)
	}
	return &CloudIngressClient{
		origin:      origin.String(),
		ingressRoot: ingressRoot,
		session:     ingressSession,
		http:        cloudNoRedirectHTTPClient(httpClient, 2*time.Minute),
		maxRequest:  cloudIngressMaxRequestBytes,
		maxResponse: cloudIngressMaxResponseBytes,
	}, nil
}

func (c *CloudIngressClient) Do(
	ctx context.Context,
	endpoint CloudIngressEndpoint,
	deviceCredential string,
	body []byte,
) (*CloudIngressResponse, error) {
	contract, ok := cloudIngressEndpointContracts[endpoint]
	if c == nil || c.http == nil || !ok || int64(len(body)) > c.maxRequest {
		return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud ingress request", nil)
	}
	if contract.method == http.MethodGet && len(body) != 0 {
		return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud ingress request", nil)
	}
	if contract.deviceBearer {
		if parseDeviceCredential(deviceCredential) == nil {
			return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud ingress device request", nil)
		}
	} else if deviceCredential != "" {
		return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud ingress outer request", nil)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		contract.method,
		c.origin+c.ingressRoot+contract.path,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, newCloudError(CloudErrInvalidInput, "prepare Cloud ingress request", err)
	}
	request.AddCookie(&http.Cookie{Name: "ingress_session", Value: c.session})
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if contract.deviceBearer {
		request.Header.Set("Authorization", "Bearer "+deviceCredential)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if isCloudOutcomeSensitiveIngressEndpoint(endpoint) {
			return nil, newCloudError(
				CloudErrOutcomeUnknown,
				"dispatch Cloud Relay request",
				cloudRequestError("send Cloud ingress request", err),
			)
		}
		return nil, cloudRequestError("send Cloud ingress request", err)
	}
	relayVersion := ""
	relayVersionValues := response.Header.Values(relayVersionHeader)
	if len(relayVersionValues) == 1 {
		relayVersion = strings.TrimSpace(relayVersionValues[0])
	}
	reachedRelay := len(relayVersionValues) == 1 &&
		validIdentifier(relayVersion, 128)
	outcomeSensitive := isCloudOutcomeSensitiveIngressEndpoint(endpoint)
	if isHTTPRedirect(response.StatusCode) {
		response.Body.Close()
		if outcomeSensitive {
			return nil, newCloudHTTPError(
				CloudErrOutcomeUnknown,
				"dispatch Cloud Relay request",
				response.StatusCode,
				false,
			)
		}
		return nil, newCloudHTTPError(CloudErrRedirectRejected, "send Cloud ingress request", response.StatusCode, false)
	}
	if response.StatusCode == http.StatusUnauthorized {
		if contract.deviceBearer && reachedRelay {
			response.Body.Close()
			return nil, newCloudHTTPError(CloudErrDeviceRejected, "authorize Cloud ingress device", response.StatusCode, false)
		}
		if !reachedRelay {
			response.Body.Close()
			return nil, newCloudHTTPError(CloudErrOuterSessionExpired, "authorize Cloud ingress session", response.StatusCode, true)
		}
	}
	if outcomeSensitive {
		if !reachedRelay && response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			return nil, newCloudHTTPError(
				CloudErrIngressUnavailable,
				"reach HA NOVA through Cloud ingress",
				response.StatusCode,
				true,
			)
		}
		if !reachedRelay ||
			response.StatusCode >= http.StatusInternalServerError {
			response.Body.Close()
			return nil, newCloudHTTPError(
				CloudErrOutcomeUnknown,
				"dispatch Cloud Relay request",
				response.StatusCode,
				false,
			)
		}
	}
	if !reachedRelay && (response.StatusCode == http.StatusNotFound ||
		response.StatusCode == http.StatusBadGateway ||
		response.StatusCode == http.StatusServiceUnavailable) {
		response.Body.Close()
		return nil, newCloudHTTPError(CloudErrIngressUnavailable, "reach HA NOVA through Cloud ingress", response.StatusCode, true)
	}
	return &CloudIngressResponse{
		StatusCode:   response.StatusCode,
		ContentType:  response.Header.Get("Content-Type"),
		RelayVersion: relayVersion,
		RetryAfter:   strings.TrimSpace(response.Header.Get("Retry-After")),
		ReachedRelay: reachedRelay,
		Body: &cloudIngressLimitedBody{
			ReadCloser:       response.Body,
			remaining:        c.maxResponse,
			outcomeSensitive: outcomeSensitive,
		},
	}, nil
}

type cloudIngressLimitedBody struct {
	io.ReadCloser
	remaining        int64
	exceeded         bool
	outcomeSensitive bool
}

func (b *cloudIngressLimitedBody) Read(buffer []byte) (int, error) {
	if b.exceeded {
		return 0, b.readError(
			newCloudError(
				CloudErrResponseTooLarge,
				"read Cloud ingress response",
				nil,
			),
		)
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if b.remaining == 0 {
		var probe [1]byte
		count, err := b.ReadCloser.Read(probe[:])
		if count > 0 {
			b.exceeded = true
			return 0, b.readError(
				newCloudError(
					CloudErrResponseTooLarge,
					"read Cloud ingress response",
					nil,
				),
			)
		}
		return b.readResult(0, err)
	}
	limit := int64(len(buffer))
	if limit > b.remaining {
		limit = b.remaining
	}
	count, err := b.ReadCloser.Read(buffer[:limit])
	b.remaining -= int64(count)
	return b.readResult(count, err)
}

func (b *cloudIngressLimitedBody) readResult(count int, err error) (int, error) {
	if err != nil && !errors.Is(err, io.EOF) {
		return count, b.readError(cloudRequestError(
			"read Cloud ingress response",
			err,
		))
	}
	return count, err
}

func (b *cloudIngressLimitedBody) readError(err error) error {
	if b.outcomeSensitive {
		return newCloudError(
			CloudErrOutcomeUnknown,
			"read Cloud Relay response",
			err,
		)
	}
	return err
}

func isCloudOutcomeSensitiveIngressEndpoint(endpoint CloudIngressEndpoint) bool {
	return endpoint == CloudEndpointDeviceRevoke ||
		isCloudFunctionalIngressEndpoint(endpoint)
}

func isCloudFunctionalIngressEndpoint(endpoint CloudIngressEndpoint) bool {
	switch endpoint {
	case CloudEndpointHealth,
		CloudEndpointWS,
		CloudEndpointCore,
		CloudEndpointFiles,
		CloudEndpointBackups:
		return true
	default:
		return false
	}
}
