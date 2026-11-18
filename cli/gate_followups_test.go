package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func gateFollowupSession(t *testing.T, status int, body string) *relaySession {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s := relaySessionOn(runtimePaths{}, runtimeConfig{}, relayTransportSelection{BaseURL: server.URL, Client: server.Client(), Credential: "secret-token", Via: relayViaLocal}, ctx, cancel)
	t.Cleanup(s.close)
	return s
}

// MAR-242: only a 2xx with ok: true is a gate result; anything else keeps the
// envelope's code, or one from the status.
func TestGateCallTakesOnlyA2xxOKAsAResult(t *testing.T) {
	for _, c := range []struct {
		status     int
		body, code string
	}{
		{500, `{"ok":true,"data":{}}`, "HTTP_500"},
		{409, `{"ok":true,"error":{"code":"GATE_BUSY"}}`, "GATE_BUSY"},
		{502, `{"ok":false}`, "HTTP_502"},
		{502, `<html>Bad gateway</html>`, "HTTP_502"},
		{500, `{"error":{"code":"GATE_UNAVAILABLE"}}`, "HTTP_500"},
		{409, `{"ok":"no","error":{"code":"GATE_NO_CHANNEL"}}`, "HTTP_409"},
	} {
		var into struct{}
		err := gateFollowupSession(t, c.status, c.body).gateCall(context.Background(), gateAskRoute, 5*time.Second, map[string]any{"op": "wait"}, &into)
		var call *relayCallError
		if !errors.As(err, &call) || call.code != c.code || call.status != c.status {
			t.Fatalf("HTTP %d %s: %v", c.status, c.body, err)
		}
	}
	var into struct {
		State string `json:"state"`
	}
	if err := gateFollowupSession(t, 200, `{"ok":true,"data":{"state":"waiting"}}`).gateCall(context.Background(), gateAskRoute, 5*time.Second, map[string]any{"op": "wait"}, &into); err != nil || into.State != "waiting" {
		t.Fatalf("a 200 with ok: true: %v %+v", err, into)
	}
}

// MAR-242: an unread /gate/v1/me keeps why, and never the credential.
func TestGateMeKeepsWhyItCouldNotBeRead(t *testing.T) {
	for _, c := range []struct {
		status     int
		body, want string
	}{
		{503, `{"ok":true,"data":{"window":true,"why":"on"}}`, "HTTP 503"},
		{200, `not json`, gateMeRoute},
		{200, `{"ok":false}`, "ok: false"},
		{200, `{"ok":true,"data":{"why":"later"}}`, "unknown state"},
	} {
		s := gateFollowupSession(t, c.status, c.body)
		s.gate = &relayGate{checked: true, on: true, window: true}
		me := s.readGateMe()
		if me.why != "" || me.err == nil || !strings.Contains(me.err.Error(), c.want) || strings.Contains(me.err.Error(), "secret-token") {
			t.Fatalf("HTTP %d %s: why %q, err %v", c.status, c.body, me.why, me.err)
		}
	}
}

func TestGateMeTransportCauseNamesNoAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s := relaySessionOn(runtimePaths{}, runtimeConfig{}, relayTransportSelection{BaseURL: "http://127.0.0.1:1/x?key=secret-in-url", Client: &http.Client{}, Credential: "secret-token", Via: relayViaLocal}, ctx, cancel)
	t.Cleanup(s.close)
	s.gate = &relayGate{checked: true, on: true, window: true}
	me := s.readGateMe()
	if me.err == nil || !strings.Contains(me.err.Error(), gateMeRoute) || strings.Contains(me.err.Error(), "secret") {
		t.Fatalf("cause %v", me.err)
	}
}
