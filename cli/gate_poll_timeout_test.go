package main

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// The App holds one gate wait poll for 25s (POLL_MS in
// nova/src/http/gate-ask-table.ts). The functional client used to carry the
// pairing client's 20s timeout, so the poll failed and gateWait recorded the
// ask as lost (CONFIRM_UNAVAILABLE) while the page was still open.
const appWaitPollHold = 25 * time.Second

func TestFunctionalTransportWaitPollOutlastsTheAppHold(t *testing.T) {
	resetServerProfileSelection(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	pairedServer := httptest.NewTLSServer(holdGateWait(appWaitPollHold))
	t.Cleanup(pairedServer.Close)
	legacyServer := httptest.NewServer(holdGateWait(appWaitPollHold))
	t.Cleanup(legacyServer.Close)

	sum := sha256.Sum256(pairedServer.Certificate().RawSubjectPublicKeyInfo)
	pin := pairB64.EncodeToString(sum[:])
	credential := validCredential(7)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("legacy-token"); err != nil {
		t.Fatal(err)
	}

	pairedBase, pairedClient, pairedCred, pairedDevice, err := relayFunctionalTransport(runtimeConfig{
		RelayBaseURL:       "http://127.0.0.1:8791",
		RelaySecureBaseURL: pairedServer.URL,
		RelaySpkiPin:       pin,
	})
	if err != nil || !pairedDevice || pairedBase != pairedServer.URL || pairedCred != credential || pairedClient == nil || pairedClient.Timeout != 0 {
		t.Fatalf("paired transport: base=%q device=%v timeout=%v err=%v", pairedBase, pairedDevice, clientTimeout(pairedClient), err)
	}
	bridgeBase, bridgeClient, _, bridgeDevice, err := relayFunctionalTransport(runtimeConfig{
		RelaySecureBaseURL: pairedServer.URL,
		RelaySpkiPin:       pin,
		bridgeCredential:   credential,
	})
	if err != nil || !bridgeDevice || bridgeBase != pairedServer.URL || bridgeClient == nil || bridgeClient.Timeout != 0 {
		t.Fatalf("bridge transport: base=%q device=%v timeout=%v err=%v", bridgeBase, bridgeDevice, clientTimeout(bridgeClient), err)
	}
	legacyBase, legacyClient, legacyCred, legacyDevice, err := relayFunctionalTransport(runtimeConfig{RelayBaseURL: legacyServer.URL})
	if err != nil || legacyDevice || legacyBase != legacyServer.URL || legacyCred != "legacy-token" || legacyClient == nil || legacyClient == httpClient || legacyClient.Timeout != 0 {
		t.Fatalf("legacy transport: base=%q device=%v timeout=%v shared=%v err=%v", legacyBase, legacyDevice, clientTimeout(legacyClient), legacyClient == httpClient, err)
	}
	if legacyClient.Transport != httpClient.Transport {
		t.Fatal("legacy functional client must keep the shared dial transport")
	}
	if httpClient.Timeout != time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)) || spkiPinnedClient(pin).Timeout != pairingHTTPTimeout || demoHTTPClient().Timeout != 0 {
		t.Fatalf("shared client timeout %v, pairing client timeout %v, demo client timeout %v", httpClient.Timeout, spkiPinnedClient(pin).Timeout, demoHTTPClient().Timeout)
	}

	type poll struct {
		name, state string
		elapsed     time.Duration
		err         error
	}
	results := make(chan poll, 2)
	for _, tc := range []struct {
		name, base, cred string
		client           *http.Client
	}{
		{"paired", pairedBase, pairedCred, pairedClient},
		{"legacy", legacyBase, legacyCred, legacyClient},
	} {
		go func(name, base, cred string, client *http.Client) {
			state, elapsed, callErr := gateWaitPoll(base, client, cred)
			results <- poll{name, state, elapsed, callErr}
		}(tc.name, tc.base, tc.cred, tc.client)
	}
	health := make(chan poll, 1)
	go func() {
		start := time.Now()
		_, callErr := fetchRelayHealthBound(pairedClient, pairedBase, pairedCred)
		health <- poll{elapsed: time.Since(start), err: callErr}
	}()
	for range 2 {
		got := <-results
		if got.err != nil || got.state != "waiting" || got.elapsed <= pairingHTTPTimeout || got.elapsed >= gatePollTimeout {
			t.Errorf("%s wait poll: state=%q elapsed=%s err=%v (an error here is what gateWait records as lost)", got.name, got.state, got.elapsed, got.err)
		}
	}
	select {
	case got := <-health:
		if got.err == nil || got.elapsed < pairingHTTPTimeout-time.Second || got.elapsed > pairingHTTPTimeout+3*time.Second {
			t.Fatalf("functional health read with no deadline: err=%v elapsed=%s, want a timeout around %s", got.err, got.elapsed, pairingHTTPTimeout)
		}
	case <-time.After(pairingHTTPTimeout + 5*time.Second):
		t.Fatal("functional health read with no deadline hung")
	}
}

func TestPairingClientStillTimesOutAtThePairingBound(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := time.NewTimer(pairingHTTPTimeout + 10*time.Second)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
		case <-timer.C:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	pin := pairB64.EncodeToString(sum[:])
	start := time.Now()
	// activate and revoke each send one of these, with no request context.
	_, err := pairAuthedPost(spkiPinnedClient(pin), server.URL+"/auth/device/activate", validCredential(3))
	elapsed := time.Since(start)
	if err == nil || elapsed < pairingHTTPTimeout-time.Second || elapsed > pairingHTTPTimeout+3*time.Second {
		t.Fatalf("pairing request err=%v elapsed=%s, want a timeout around %s", err, elapsed, pairingHTTPTimeout)
	}
}

func gateWaitPoll(base string, client *http.Client, credential string) (string, time.Duration, error) {
	session := &relaySession{transport: relayTransportSelection{
		BaseURL: base, Client: client, Credential: credential, Via: relayViaLocal,
	}}
	var answer struct {
		State string `json:"state"`
	}
	start := time.Now()
	err := session.gateCall(context.Background(), gateAskRoute, gatePollTimeout, map[string]any{"ask_id": "ask-1", "op": "wait"}, &answer)
	return answer.State, time.Since(start), err
}

// holdGateWait answers a gate wait poll after hold, and holds /health open
// until the client gives up, so a missing deadline cannot hang the test.
func holdGateWait(hold time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			timer := time.NewTimer(2 * time.Minute)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
			case <-timer.C:
			}
			return
		}
		if r.URL.Path != "/"+gateAskRoute {
			http.NotFound(w, r)
			return
		}
		time.Sleep(hold)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"state":"waiting"}}`))
	})
}
