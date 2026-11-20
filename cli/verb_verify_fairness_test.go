package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// verifySession serves /core state reads after delay; the stuck entity never
// answers until its read gives up. peak records the most reads in flight.
func verifySession(t *testing.T, delay time.Duration, stuck string, peak *atomic.Int32) *relaySession {
	t.Helper()
	var inFlight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
		}
		raw, _ := io.ReadAll(r.Body)
		var payload struct{ Path string }
		_ = json.Unmarshal(raw, &payload)
		id := strings.TrimPrefix(payload.Path, "/api/states/")
		if id == stuck {
			<-r.Context().Done()
			return
		}
		time.Sleep(delay)
		body, _ := json.Marshal(map[string]any{"entity_id": id, "state": "on", "attributes": map[string]any{}})
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"status": 200, "body": json.RawMessage(body)}})
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return &relaySession{
		transport: relayTransportSelection{BaseURL: srv.URL, Client: srv.Client(), Via: relayViaLocal},
		ctx:       ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{},
	}
}

func lightProbes(n int) []probe {
	probes := make([]probe, n)
	for i := range probes {
		probes[i] = probe{entityID: fmt.Sprintf("light.l%02d", i), checks: []check{{kind: "literal", want: "on"}}}
	}
	return probes
}

func shortVerifyWindow(t *testing.T, window time.Duration) {
	t.Helper()
	interval, fast := verifyInterval, verifyWindowFast
	t.Cleanup(func() { verifyInterval, verifyWindowFast = interval, fast })
	verifyInterval, verifyWindowFast = 10*time.Millisecond, window
}

// PERF-01: ten lights at 120 ms a read fit a 600 ms window only when they are
// read together (one after the other, the last ones got no read at all). The
// send cap of eight still holds, and every row keeps its input position.
func TestPollProbesReadsEveryTargetWithinTheWindow(t *testing.T) {
	shortVerifyWindow(t, 600*time.Millisecond)
	var peak atomic.Int32
	s := verifySession(t, 120*time.Millisecond, "", &peak)
	probes := lightProbes(10)
	start := time.Now()
	rows := pollProbes(s, probes)
	for i, row := range rows {
		if row.EntityID != probes[i].entityID || row.Outcome != "verified" {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
	if took := time.Since(start); took > verifyWindowFast {
		t.Fatalf("took %v, window %v", took, verifyWindowFast)
	}
	if p := peak.Load(); p > relaySessionMaxConcurrent || p < 2 {
		t.Fatalf("peak concurrent reads %d", p)
	}
}

// A read waiting for a send slot ends by its window's absolute deadline: a
// target that never answers is unverified when the window closes, and the
// others verify.
func TestPollProbesEndsQueuedReadsByTheWindow(t *testing.T) {
	shortVerifyWindow(t, 400*time.Millisecond)
	var peak atomic.Int32
	s := verifySession(t, 20*time.Millisecond, "light.l03", &peak)
	start := time.Now()
	rows := pollProbes(s, lightProbes(12))
	if took := time.Since(start); took > verifyWindowFast+300*time.Millisecond {
		t.Fatalf("took %v, window %v", took, verifyWindowFast)
	}
	for i, row := range rows {
		want := "verified"
		if i == 3 {
			want = "unverified"
		}
		if row.Outcome != want {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
}
