package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// A read sent ahead of its consumer (a warm read, a prefetch, a batch) that
// gets no answer is a failure nobody used: it is never handed on, and the
// consumer reads again. A consumer's own failure stays for the session, so
// a plan's evaluation, basis, and replay agree (#1299: the first lamp call
// after an App update was refused with SERVICE_LIST_UNREAD).

// servicesHarness fails the first fails GET /api/services with no answer
// from Home Assistant; the registry answers late, so the call plan's warm
// read of the list is sent before the engine asks for it.
func servicesHarness(t *testing.T, fails int) (*writeHarness, func() int) {
	t.Helper()
	fastVerify(t)
	f := newFakeHA()
	f.service = applies
	var mu sync.Mutex
	reads := 0
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if p["type"] == "config/entity_registry/list_for_display" {
			time.Sleep(150 * time.Millisecond)
		}
		if endpoint == "core" && p["method"] == http.MethodGet && p["path"] == "/api/services" {
			mu.Lock()
			reads++
			n := reads
			mu.Unlock()
			if n <= fails {
				return fakeRelayFailure{"UPSTREAM_HTTP_TIMEOUT", "timed out"}
			}
		}
		return f.reply(r, endpoint, p)
	}, true)
	return &writeHarness{t: t, f: f, paths: paths, log: log}, func() int { mu.Lock(); defer mu.Unlock(); return reads }
}

func TestAWarmServiceListWithNoAnswerIsReadAgain(t *testing.T) {
	h, reads := servicesHarness(t, 1)
	exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen")
	if exit != 0 || env.Status != "verified" || len(h.posts()) != 1 || reads() != 2 {
		t.Fatalf("exit %d status %s posts %d service list reads %d (want the warm one, then the engine's own): %+v", exit, env.Status, len(h.posts()), reads(), env.Error)
	}
}

func TestAServiceListTheEngineCannotReadStillRefusesAndSaysWhy(t *testing.T) {
	h, reads := servicesHarness(t, 2)
	exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen")
	wantUnlisted(t, "both reads unanswered", h, exit, env, reasonServiceListUnread)
	if detail := env.Reasons[0].Detail; detail != "UPSTREAM_HTTP_TIMEOUT (HTTP 502)" || reads() != 2 {
		t.Fatalf("detail %q, service list reads %d", detail, reads())
	}
}

func TestReadCauseNamesTheFailureWithoutTheRelayURL(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&relayCallError{endpoint: "core", status: 504, code: "UPSTREAM_HTTP_TIMEOUT", message: "http://10.0.0.2:8123 timed out"}, "UPSTREAM_HTTP_TIMEOUT (HTTP 504)"},
		{&url.Error{Op: "Post", URL: "https://relay.example:8791/ws", Err: errors.New("connection refused")}, "the Relay could not be reached"},
		{&url.Error{Op: "Post", URL: "https://relay.example:8791/ws", Err: context.DeadlineExceeded}, "no answer in time"},
	} {
		if got := readCause(tc.err); got != tc.want {
			t.Errorf("readCause(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// warmSession: a session on a home whose GET /api/services fails while
// fail is set, counting what reaches the Relay.
func warmSession(t *testing.T, f *fakeHA) (*relaySession, *fakeRelayLog) {
	t.Helper()
	paths, log := setupVerbRelayTest(t, f.reply)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	return s, log
}

func coreReads(log *fakeRelayLog, path string) int {
	n := 0
	for _, c := range log.all() {
		if c.endpoint == "core" && c.payload["path"] == path {
			n++
		}
	}
	return n
}

func TestAConsumersOwnFailureStaysForThePlan(t *testing.T) {
	f := newFakeHA()
	f.fail["/api/services"] = true
	s, log := warmSession(t, f)
	_, _, first := s.coreGET("/api/services")
	f.mu.Lock()
	f.fail["/api/services"] = false
	f.mu.Unlock()
	_, _, second := s.coreGET("/api/services")
	if first == nil || second == nil || coreReads(log, "/api/services") != 1 {
		t.Fatalf("errors %v / %v, reads %d: a consumer's failure must stay what the plan saw", first, second, coreReads(log, "/api/services"))
	}
}

func TestAFailedWarmBatchIsReadAgainByAConsumerWaitingOnIt(t *testing.T) {
	withFakeBatches(t)
	fakeRelayBatchFails.Store(true)
	f := newFakeHA()
	f.automation("a", "a_id", map[string]any{"triggers": []any{}})
	f.automation("b", "b_id", map[string]any{"triggers": []any{}})
	s, log := warmSession(t, f)
	requests := []map[string]any{loadedConfigRequest("automation.a", "automation"), loadedConfigRequest("automation.b", "automation")}
	s.readBatched(requests, 0)
	var wg sync.WaitGroup
	errs := make([]error, len(requests))
	for i, request := range requests {
		wg.Add(1)
		go func() { defer wg.Done(); _, errs[i] = s.ws(request) }()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || log.batches.Load() != 1 || configReads(log) != 2 {
		t.Fatalf("errors %v, batches %d, single reads %d: want the failed batch's reads made again, once each", errs, log.batches.Load(), configReads(log))
	}
}

func TestAWarmReadOfHomeAssistantsRefusalIsItsAnswer(t *testing.T) {
	f := newFakeHA()
	s, log := warmSession(t, f)
	request := loadedConfigRequest("automation.gone", "automation")
	_, warm := s.warmRead("ws", request)
	_, consumer := s.ws(request)
	if !isUpstreamNotFound(warm) || !isUpstreamNotFound(consumer) || configReads(log) != 1 {
		t.Fatalf("warm %v, consumer %v, reads %d: a refusal is an answer, read once", warm, consumer, configReads(log))
	}
	if strings.Contains(readCause(consumer), "http") {
		t.Fatalf("cause names a URL: %q", readCause(consumer))
	}
}
