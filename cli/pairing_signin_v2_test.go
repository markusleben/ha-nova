package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func armTestClient(t *testing.T, status int, reachedRelay bool, body string) *CloudIngressClient {
	t.Helper()
	return newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		sent, _ := io.ReadAll(request.Body)
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, CloudPathPairArm) ||
			request.Header.Get("Content-Type") != "application/json" || string(sent) != "{}" ||
			request.Header.Get("Authorization") != "" {
			t.Fatalf("arm request = %s %s %q %q", request.Method, request.URL.Path, request.Header, sent)
		}
		header := http.Header{}
		if reachedRelay {
			header.Set(relayVersionHeader, "0.9.2")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
}

func TestArmSignInPairingDecodesTheStrictResponse(t *testing.T) {
	secret := strings.Repeat("s", 43)
	armed, err := armSignInPairing(context.Background(), armTestClient(t, http.StatusOK, true,
		`{"ok":true,"data":{"pairing_secret":"`+secret+`","expires_in_s":120,"relay_instance_id":"relay-1"}}`), CloudEndpointPairArm)
	if err != nil || armed.PairingSecret != secret || armed.RelayInstanceID != "relay-1" {
		t.Fatalf("armed = %+v, %v", armed, err)
	}
}

func TestArmSignInPairingClassifiesRefusals(t *testing.T) {
	secret := strings.Repeat("s", 43)
	for name, test := range map[string]struct {
		status  int
		reached bool
		body    string
		want    error
		code    CloudErrorCode
	}{
		"older relay":  {status: http.StatusNotFound, reached: true, body: `{"ok":false,"error":{"code":"NOT_FOUND","message":"x"}}`, want: errSignInRelayUnsupported},
		"not owner":    {status: http.StatusForbidden, reached: true, body: `{"ok":false,"error":{"code":"OWNER_REQUIRED","message":"x"}}`, want: errSignInNotOwner},
		"browser":      {status: http.StatusForbidden, reached: true, body: `{"ok":false,"error":{"code":"MACHINE_REQUEST_REQUIRED","message":"x"}}`, code: CloudErrHAProtocol},
		"unavailable":  {status: http.StatusServiceUnavailable, reached: true, body: `{"ok":false,"error":{"code":"PAIRING_UNAVAILABLE","message":"x"}}`, want: errSignInRelayNotReady},
		"owner check":  {status: http.StatusServiceUnavailable, reached: true, body: `{"ok":false,"error":{"code":"OWNER_CHECK_UNAVAILABLE","message":"x"}}`, want: errSignInOwnerCheckSlow, code: CloudErrHAProtocol},
		"extra field":  {status: http.StatusOK, reached: true, body: `{"ok":true,"data":{"pairing_secret":"` + secret + `","expires_in_s":120,"relay_instance_id":"relay-1","code":"123456"}}`, code: CloudErrHAProtocol},
		"short secret": {status: http.StatusOK, reached: true, body: `{"ok":true,"data":{"pairing_secret":"123456","expires_in_s":120,"relay_instance_id":"relay-1"}}`, code: CloudErrHAProtocol},
		"not relay":    {status: http.StatusOK, reached: false, body: `{"ok":true,"data":{}}`, code: CloudErrHAProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := armSignInPairing(context.Background(), armTestClient(t, test.status, test.reached, test.body), CloudEndpointPairArm)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
			if test.code != "" && !IsCloudErrorCode(err, test.code) {
				t.Fatalf("err = %v, want code %s", err, test.code)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatalf("secret leaked into error: %v", err)
			}
		})
	}
}
