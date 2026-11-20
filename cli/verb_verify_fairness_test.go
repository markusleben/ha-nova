package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// verifySession serves /core state reads after delay with the state answer
// gives for an entity's nth read ("" never answers until the read gives up);
// peak records the most reads in flight.
func verifySession(t *testing.T, delay time.Duration, answer func(id string, n int) string, peak *atomic.Int32) *relaySession {
	t.Helper()
	var inFlight atomic.Int32
	var mu sync.Mutex
	reads := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := peak.Load(); now > old && !peak.CompareAndSwap(old, now); old = peak.Load() {
		}
		raw, _ := io.ReadAll(r.Body)
		var payload struct{ Path string }
		_ = json.Unmarshal(raw, &payload)
		id := strings.TrimPrefix(payload.Path, "/api/states/")
		mu.Lock()
		reads[id]++
		state := answer(id, reads[id])
		mu.Unlock()
		if state == "" {
			<-r.Context().Done()
			return
		}
		time.Sleep(delay)
		body, _ := json.Marshal(map[string]any{"entity_id": id, "state": state, "attributes": map[string]any{}})
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

func on(string, int) string { return "on" }

func lightProbes(n int) []probe {
	probes := make([]probe, n)
	for i := range probes {
		probes[i] = probe{entityID: fmt.Sprintf("light.l%02d", i), checks: []check{{kind: "literal", want: "on"}}}
	}
	return probes
}

func shortVerifyWindows(t *testing.T, fast, slow time.Duration) {
	t.Helper()
	interval, savedFast, savedSlow := verifyInterval, verifyWindowFast, verifyWindowSlow
	t.Cleanup(func() { verifyInterval, verifyWindowFast, verifyWindowSlow = interval, savedFast, savedSlow })
	verifyInterval, verifyWindowFast, verifyWindowSlow = 10*time.Millisecond, fast, slow
}

// PERF-01: ten lights at 120 ms a read fit a 600 ms window only when they are
// read together (one after the other, the last ones got no read at all). The
// send cap of eight still holds, and every row keeps its input position.
func TestPollProbesReadsEveryTargetWithinTheWindow(t *testing.T) {
	shortVerifyWindows(t, 600*time.Millisecond, time.Second)
	var peak atomic.Int32
	s := verifySession(t, 120*time.Millisecond, on, &peak)
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

// A stuck slow target holds no other target's next read: a light that
// answered before it settled reads again and verifies in its own window,
// while the cover that never answers is unverified when its window closes.
// No read outlasts a window by more than its 100 ms floor.
func TestPollProbesKeepsReadingPastAStuckTarget(t *testing.T) {
	shortVerifyWindows(t, 300*time.Millisecond, 800*time.Millisecond)
	var peak atomic.Int32
	s := verifySession(t, 20*time.Millisecond, func(id string, n int) string {
		switch {
		case strings.HasPrefix(id, "cover."):
			return ""
		case n == 1:
			return "off"
		}
		return "on"
	}, &peak)
	probes := append(lightProbes(9), probe{entityID: "cover.door", slow: true, checks: []check{{kind: "literal", want: "open"}}})
	probes = append([]probe{probes[9]}, probes[:9]...) // the stuck target first
	start := time.Now()
	rows := pollProbes(s, probes)
	if took := time.Since(start); took > verifyWindowSlow+150*time.Millisecond {
		t.Fatalf("took %v, slow window %v", took, verifyWindowSlow)
	}
	for i, row := range rows {
		want := "verified"
		if i == 0 {
			want = "unverified"
		}
		if row.EntityID != probes[i].entityID || row.Outcome != want {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
}
