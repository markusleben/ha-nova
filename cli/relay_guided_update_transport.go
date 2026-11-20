package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
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

// candidate: the App's update entity for a refresh or an install. The
// registry is read again after the state, so an entity_id handed to another
// integration meanwhile is never joined with the NOVA row read before it.
func (route *relayUpdateRoute) candidate() (relayUpdateCandidate, string) {
	ctx := context.Background()
	candidate, reason := resolveRelayUpdateCandidateWithTransport(ctx, route.BaseURL, route.Client, route.Credential)
	if reason != "" {
		return candidate, reason
	}
	rows, reason := relayNOVAUpdateRows(ctx, route.BaseURL, route.Client, route.Credential)
	if reason != "" {
		return relayUpdateCandidate{}, reason
	}
	if !slices.Contains(rows, relayNOVAUpdateRow{candidate.EntityID, candidate.Platform, candidate.UniqueID}) {
		return relayUpdateCandidate{}, "the NOVA Relay App update entity changed while it was read — not guessing"
	}
	return candidate, ""
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
