package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const cloudDeviceRevokeMaxAttempts = 3

type CloudDeviceBindResult struct {
	DeviceID string `json:"device_id"`
	Bound    bool   `json:"bound"`
	Changed  bool   `json:"changed"`
}

type CloudDeviceActivateResult struct {
	DeviceID  string `json:"device_id"`
	Activated bool   `json:"activated"`
	Bound     bool   `json:"bound"`
	Changed   bool   `json:"changed"`
}

type CloudDeviceRevokeResult struct {
	DeviceID string `json:"device_id"`
	Revoked  bool   `json:"revoked"`
	Changed  bool   `json:"changed"`
}

func (c *CloudIngressClient) BindDevice(
	ctx context.Context,
	deviceCredential, relayInstanceID string,
) (CloudDeviceBindResult, error) {
	var result CloudDeviceBindResult
	if err := c.deviceLifecycleRequest(
		ctx,
		CloudEndpointDeviceBind,
		deviceCredential,
		relayInstanceID,
		&result,
	); err != nil {
		return CloudDeviceBindResult{}, err
	}
	if !validIdentifier(result.DeviceID, 256) || !result.Bound {
		return CloudDeviceBindResult{}, newCloudError(CloudErrHAProtocol, "validate Cloud device binding", nil)
	}
	return result, nil
}

func (c *CloudIngressClient) ActivateDevice(
	ctx context.Context,
	pendingDeviceCredential, relayInstanceID string,
) (CloudDeviceActivateResult, error) {
	var result CloudDeviceActivateResult
	if err := c.deviceLifecycleRequest(
		ctx,
		CloudEndpointDeviceActivate,
		pendingDeviceCredential,
		relayInstanceID,
		&result,
	); err != nil {
		return CloudDeviceActivateResult{}, err
	}
	if !validIdentifier(result.DeviceID, 256) || !result.Activated || !result.Bound {
		return CloudDeviceActivateResult{}, newCloudError(CloudErrHAProtocol, "validate Cloud device activation", nil)
	}
	return result, nil
}

func (c *CloudIngressClient) RevokeDevice(
	ctx context.Context,
	deviceCredential, relayInstanceID string,
) (CloudDeviceRevokeResult, error) {
	parsed := parseDeviceCredential(deviceCredential)
	if parsed == nil {
		return CloudDeviceRevokeResult{}, newCloudError(
			CloudErrInvalidInput,
			"prepare Cloud device revocation",
			nil,
		)
	}
	body, err := cloudDeviceLifecycleBody(relayInstanceID)
	if err != nil {
		return CloudDeviceRevokeResult{}, err
	}
	var result CloudDeviceRevokeResult
	for attempt := 0; attempt < cloudDeviceRevokeMaxAttempts; attempt++ {
		result = CloudDeviceRevokeResult{}
		err = c.deviceLifecycleRequestBody(
			ctx,
			CloudEndpointDeviceRevoke,
			deviceCredential,
			body,
			&result,
		)
		if err == nil {
			if result.DeviceID != parsed.deviceID || !result.Revoked {
				return CloudDeviceRevokeResult{}, newCloudError(
					CloudErrHAProtocol,
					"validate Cloud device revocation",
					nil,
				)
			}
			return result, nil
		}
		if attempt+1 == cloudDeviceRevokeMaxAttempts ||
			!retryCloudDeviceRevocation(err) {
			return CloudDeviceRevokeResult{}, err
		}
		if err := waitCloudDeviceRevocationRetry(ctx, attempt); err != nil {
			return CloudDeviceRevokeResult{}, err
		}
	}
	return CloudDeviceRevokeResult{}, err
}

func (c *CloudIngressClient) deviceLifecycleRequest(
	ctx context.Context,
	endpoint CloudIngressEndpoint,
	deviceCredential, relayInstanceID string,
	result any,
) error {
	body, err := cloudDeviceLifecycleBody(relayInstanceID)
	if err != nil {
		return err
	}
	return c.deviceLifecycleRequestBody(
		ctx,
		endpoint,
		deviceCredential,
		body,
		result,
	)
}

func cloudDeviceLifecycleBody(relayInstanceID string) ([]byte, error) {
	if !validIdentifier(relayInstanceID, 256) {
		return nil, newCloudError(
			CloudErrInvalidInput,
			"prepare Cloud device lifecycle request",
			nil,
		)
	}
	body, err := json.Marshal(struct {
		RelayInstanceID string `json:"relay_instance_id"`
	}{RelayInstanceID: relayInstanceID})
	if err != nil {
		return nil, newCloudError(
			CloudErrInvalidInput,
			"prepare Cloud device lifecycle request",
			err,
		)
	}
	return body, nil
}

func (c *CloudIngressClient) deviceLifecycleRequestBody(
	ctx context.Context,
	endpoint CloudIngressEndpoint,
	deviceCredential string,
	body []byte,
	result any,
) error {
	response, err := c.Do(ctx, endpoint, deviceCredential, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := readCloudResponse(response.Body, 64<<10, "read Cloud device lifecycle response")
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return decodeCloudLifecycleError(response.StatusCode, data)
	}
	envelope := struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || !envelope.OK {
		return newCloudError(CloudErrHAProtocol, "decode Cloud device lifecycle response", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud device lifecycle response", err)
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	dataDecoder.DisallowUnknownFields()
	if err := dataDecoder.Decode(result); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud device lifecycle result", err)
	}
	if err := ensureJSONEOF(dataDecoder); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Cloud device lifecycle result", err)
	}
	return nil
}

func retryCloudDeviceRevocation(err error) bool {
	var cloudErr *CloudError
	return errors.As(err, &cloudErr) &&
		cloudErr.Code == CloudErrOutcomeUnknown &&
		(cloudErr.StatusCode == 0 ||
			cloudErr.StatusCode >= http.StatusInternalServerError)
}

func waitCloudDeviceRevocationRetry(
	ctx context.Context,
	attempt int,
) error {
	delay := 50 * time.Millisecond
	if attempt > 0 {
		delay = 150 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func decodeCloudLifecycleError(status int, data []byte) error {
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	switch envelope.Error.Code {
	case "CLOUD_USER_CONFLICT":
		return newCloudHTTPError(CloudErrDeviceUserConflict, "bind Cloud device", status, false)
	case "RELAY_INSTANCE_MISMATCH":
		return newCloudHTTPError(CloudErrRelayInstance, "bind Cloud device", status, false)
	default:
		return newCloudHTTPError(CloudErrHAProtocol, "bind Cloud device", status, status >= 500)
	}
}
