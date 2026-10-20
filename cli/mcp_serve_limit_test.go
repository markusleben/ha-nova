package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// --rate-limit: n requests per minute per HA user, then a JSON-RPC 429 that
// never reaches the Relay; another user has a window of their own.
func TestServeRateLimit(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`","`+userB+`"]}`)
	s.b.limiter.limit = 2
	list := `{"jsonrpc":"2.0","id":7,"method":"tools/list"}`
	for i := 0; i < 2; i++ {
		if status, _ := s.raw(http.MethodPost, s.as(userA), list); status != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, status)
		}
	}
	status, body := s.raw(http.MethodPost, s.as(userA), list)
	if status != http.StatusTooManyRequests || !strings.Contains(body, `"id":7,`) || !strings.Contains(body, "Wait a minute") {
		t.Fatalf("over the limit: HTTP %d %s", status, body)
	}
	if status, _ := s.raw(http.MethodPost, s.as(userB), list); status != http.StatusOK {
		t.Fatalf("another user: %d", status)
	}
	s.b.limiter.mu.Lock()
	w := s.b.limiter.hits[s.userKey(userA)]
	w.start = w.start.Add(-time.Minute)
	s.b.limiter.hits[s.userKey(userA)] = w
	s.b.limiter.mu.Unlock()
	if status, _ := s.raw(http.MethodPost, s.as(userA), list); status != http.StatusOK {
		t.Fatalf("next minute: %d", status)
	}
	if !strings.Contains(s.logs.String(), "http=429") {
		t.Fatalf("log: %s", s.logs.String())
	}
}

func (s *serveHarness) userKey(user string) string {
	call, _, _ := s.b.policyFor(user)
	return call.key
}
