package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

// relayUpdateRoute is the one route a guided NOVA App update reads and
// writes over: home, or Home Assistant Link (relayNoticeTransport).
type relayUpdateRoute struct {
	relayTransportSelection
	cfg config
}

// core posts one request through the Relay's /core proxy and returns the raw
// envelope body.
func (route *relayUpdateRoute) core(method, path string, requestBody []byte) ([]byte, error) {
	return relayCoreRequestWithTransport(context.Background(), route.BaseURL, route.Client, route.Credential, method, path, requestBody)
}

func (route *relayUpdateRoute) candidate() (relayUpdateCandidate, string) {
	return resolveRelayUpdateCandidateWithTransport(context.Background(), route.BaseURL, route.Client, route.Credential)
}

// reopenAfter: over Link, a request Home Assistant turned down because the
// Ingress session is gone gets a new Link route: a fresh sign-in and session,
// with the same Relay and user identity checks. Opening one fails while the
// App restarts; the next poll tries again.
func (route *relayUpdateRoute) reopenAfter(err error) {
	if route.Via != relayViaCloud || !IsCloudErrorCode(err, CloudErrOuterSessionExpired) {
		return
	}
	if fresh, ok := relayNoticeTransport(context.Background(), route.cfg); ok && fresh.Via == relayViaCloud {
		route.relayTransportSelection = fresh
	}
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
