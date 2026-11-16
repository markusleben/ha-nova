package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// #1183: the App holds one gate wait poll for POLL_MS before it answers
// "waiting". The CLI's own wait on that poll must outlast it with room for a
// slow route, or every typed confirm answered later than that is lost.
func TestGatePollOutlastsTheAppHold(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "nova", "src", "http", "gate-ask-table.ts"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`const POLL_MS = ([0-9_]+);`).FindSubmatch(source)
	if match == nil {
		t.Fatal("gate-ask-table.ts no longer declares POLL_MS; update this guard with it")
	}
	ms, err := strconv.Atoi(strings.ReplaceAll(string(match[1]), "_", ""))
	if err != nil {
		t.Fatal(err)
	}
	if hold := time.Duration(ms) * time.Millisecond; gatePollTimeout < hold+10*time.Second {
		t.Fatalf("gatePollTimeout %s must outlast the App's hold of %s by 10 s", gatePollTimeout, hold)
	}
}

// Every session request carries its own deadline, so no client-wide timeout
// may cut one first: the paired client's 20 s cut the App's 25 s wait poll
// and the ask was reported lost (#1183). The caller's client keeps its own
// bound for the calls outside a session.
func TestSessionRequestsOutlastTheClientTimeout(t *testing.T) {
	const hold = 300 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(hold)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"state":"waiting"}}`))
	}))
	t.Cleanup(server.Close)
	short := &http.Client{Timeout: hold / 3}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s := relaySessionOn(runtimePaths{}, runtimeConfig{}, relayTransportSelection{BaseURL: server.URL, Client: short, Credential: "token", Via: relayViaLocal}, ctx, cancel)
	t.Cleanup(s.close)

	var answer struct {
		State string `json:"state"`
	}
	if err := s.gateCall(context.Background(), gateAskRoute, 5*time.Second, map[string]any{"ask_id": "ask-1", "op": "wait"}, &answer); err != nil || answer.State != "waiting" {
		t.Fatalf("gate wait poll held %s behind a %s client timeout: state=%q err=%v (an error is what gateWait records as lost)", hold, short.Timeout, answer.State, err)
	}
	if _, err := s.sendWriteOnce("core", []byte(`{"method":"POST","path":"/api/services/light/turn_on"}`), ""); err != nil {
		t.Fatalf("write held %s behind a %s client timeout: %v", hold, short.Timeout, err)
	}
	if short.Timeout != hold/3 {
		t.Fatalf("the caller's client timeout changed to %s", short.Timeout)
	}
}

// The real constructor goes through relaySessionOn: a legacy session drops
// the shared client's 15 s bound, which itself stays for the calls outside a
// session (relay health, setup, doctor).
func TestSessionFromConfigCarriesNoClientTimeout(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	if s.transport.Client.Timeout != 0 || s.transport.Client == httpClient {
		t.Fatalf("session client timeout %s (shared client: %v)", s.transport.Client.Timeout, s.transport.Client == httpClient)
	}
	if want := time.Duration(defaultRelayMaxTimeSeconds * float64(time.Second)); httpClient.Timeout != want {
		t.Fatalf("shared relay client timeout %s, want %s", httpClient.Timeout, want)
	}
}
