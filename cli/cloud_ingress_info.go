package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

type CloudRelayInfo struct {
	ProtocolVersion string                 `json:"protocol_version"`
	RelayInstanceID string                 `json:"relay_instance_id"`
	RelayVersion    string                 `json:"relay_version"`
	Capabilities    CloudRelayCapabilities `json:"capabilities"`
}

type CloudRelayCapabilities struct {
	DeviceUserBinding bool     `json:"device_user_binding"`
	PairingV2         bool     `json:"pairing_v2"`
	FunctionalRoutes  []string `json:"functional_routes"`
	CleanupRoutes     []string `json:"cleanup_routes"`
}

func (info CloudRelayInfo) RemoteEnabled() bool {
	return info.Capabilities.DeviceUserBinding &&
		info.Capabilities.PairingV2
}

func (c *CloudIngressClient) RelayInfo(
	ctx context.Context,
) (CloudRelayInfo, error) {
	response, err := c.Do(ctx, CloudEndpointRelayInfo, "", nil)
	if err != nil {
		return CloudRelayInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !response.ReachedRelay {
		return CloudRelayInfo{}, newCloudHTTPError(
			CloudErrIngressUnavailable,
			"read Cloud Relay info",
			response.StatusCode,
			true,
		)
	}
	data, err := readCloudResponse(
		response.Body,
		64<<10,
		"read Cloud Relay info",
	)
	if err != nil {
		return CloudRelayInfo{}, err
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data CloudRelayInfo `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return CloudRelayInfo{}, newCloudError(
			CloudErrHAProtocol,
			"decode Cloud Relay info",
			err,
		)
	}
	info := envelope.Data
	if err := validateCloudRelayInfo(envelope.OK, info, decoder); err != nil {
		return CloudRelayInfo{}, err
	}
	return info, nil
}

func validateCloudRelayInfo(
	envelopeOK bool,
	info CloudRelayInfo,
	decoder *json.Decoder,
) error {
	if err := ensureJSONEOF(decoder); err != nil ||
		!envelopeOK ||
		info.ProtocolVersion != "v1" ||
		!validIdentifier(info.RelayInstanceID, 256) ||
		!validIdentifier(info.RelayVersion, 128) ||
		len(info.Capabilities.FunctionalRoutes) > 128 ||
		len(info.Capabilities.CleanupRoutes) > 128 {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Cloud Relay info",
			err,
		)
	}
	functionalRoutes, err := validateCloudRelayRouteNames(
		info.Capabilities.FunctionalRoutes,
	)
	if err != nil {
		return err
	}
	cleanupRoutes, err := validateCloudRelayRouteNames(
		info.Capabilities.CleanupRoutes,
	)
	if err != nil {
		return err
	}
	if _, ok := cleanupRoutes["device_revoke_self"]; !ok {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Cloud Relay cleanup capabilities",
			nil,
		)
	}
	if info.Capabilities.DeviceUserBinding !=
		info.Capabilities.PairingV2 {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Cloud Relay capability state",
			nil,
		)
	}
	if !info.Capabilities.DeviceUserBinding {
		if len(functionalRoutes) != 0 {
			return newCloudError(
				CloudErrHAProtocol,
				"validate disabled Cloud Relay capabilities",
				nil,
			)
		}
		return nil
	}
	for _, required := range []string{
		"health",
		"ws",
		"core",
		"files",
		"backups",
	} {
		if _, ok := functionalRoutes[required]; !ok {
			return newCloudError(
				CloudErrHAProtocol,
				"validate Cloud Relay info",
				nil,
			)
		}
	}
	return nil
}

func validateCloudRelayRouteNames(
	values []string,
) (map[string]struct{}, error) {
	routes := make(map[string]struct{}, len(values))
	for _, route := range values {
		if !validIdentifier(route, 128) {
			return nil, newCloudError(
				CloudErrHAProtocol,
				"validate Cloud Relay capabilities",
				nil,
			)
		}
		routes[route] = struct{}{}
	}
	return routes, nil
}
