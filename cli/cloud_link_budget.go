package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// The Home Assistant Link route's own budgets and its one bounded retry
// (docs/work/2026-11-03-link-route-timeouts-spec.md). Measured 2026-11-03:
// TCP plus TLS to a Link origin takes 0.1–0.3 s; a whole Link login 1–4 s,
// once 6.9 s.
const (
	linkConnectTimeout      = 5 * time.Second // one TCP plus TLS connection to the Link origin
	linkLoginRetryMinLeft   = 3 * time.Second // a second login needs about this much
	linkRequestRetryMinLeft = time.Second
	linkRouteMemoryTTL      = 2 * time.Minute
)

// Variables so tests can shrink them.
var (
	linkLoginAttemptTimeout = 10 * time.Second
	linkRetryPause          = 500 * time.Millisecond
)

type relayConnectBudgetKey struct{}

// withRelayConnectBudget bounds each connection opened under ctx (relay
// health --connect-timeout): every Link connection and the automatic local
// preflight. The whole selection keeps the caller's deadline.
func withRelayConnectBudget(ctx context.Context, budget time.Duration) context.Context {
	return context.WithValue(ctx, relayConnectBudgetKey{}, budget)
}

// relayConnectBudget is the smaller of fallback and the budget ctx carries.
func relayConnectBudget(ctx context.Context, fallback time.Duration) time.Duration {
	if budget, ok := ctx.Value(relayConnectBudgetKey{}).(time.Duration); ok && budget > 0 && budget < fallback {
		return budget
	}
	return fallback
}

type linkConnectionTraceKey struct{}

// linkConnectionTrace records, for one Link request, whether opening a
// connection failed (DNS, TCP, or TLS) and whether one was ever obtained.
type linkConnectionTrace struct {
	setupFailed atomic.Bool
	gotConn     atomic.Bool
}

// neverConnected: opening a connection failed and none was obtained, so not
// one byte of the request was written (R2). Without the Link transport's
// record (any other RoundTripper) it is false: no proof, no retry.
func (t *linkConnectionTrace) neverConnected() bool {
	return t.setupFailed.Load() && !t.gotConn.Load()
}

func traceLinkConnection(ctx context.Context) (context.Context, *linkConnectionTrace) {
	trace := &linkConnectionTrace{}
	ctx = context.WithValue(ctx, linkConnectionTraceKey{}, trace)
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { trace.gotConn.Store(true) },
	}), trace
}

// noteLinkSetupFailure marks the request whose context ctx carries (the dial
// context keeps its values) when opening its connection failed.
func noteLinkSetupFailure(ctx context.Context, err error) {
	if trace, ok := ctx.Value(linkConnectionTraceKey{}).(*linkConnectionTrace); ok && err != nil {
		trace.setupFailed.Store(true)
	}
}

// newLinkHTTPTransport carries every Link request: TCP plus TLS get
// linkConnectTimeout (or a smaller --connect-timeout), a failure to connect
// is recorded for R2, and a session's concurrent reads keep their
// connections.
func newLinkHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false // Home Assistant speaks HTTP/1.1; a WebSocket upgrade needs it
	transport.MaxIdleConnsPerHost = relaySessionMaxConcurrent
	transport.TLSHandshakeTimeout = linkConnectTimeout // through a proxy; DialTLSContext covers the rest
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, relayConnectBudget(ctx, linkConnectTimeout))
		defer cancel()
		connection, err := (&net.Dialer{}).DialContext(dialCtx, network, address)
		noteLinkSetupFailure(ctx, err)
		return connection, err
	}
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, relayConnectBudget(ctx, linkConnectTimeout))
		defer cancel()
		dialer := &tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}}
		connection, err := dialer.DialContext(dialCtx, network, address)
		noteLinkSetupFailure(ctx, err)
		return connection, err
	}
	return transport
}

// linkRetryWait pauses before the one retry; false when ctx ends first or
// leaves less than minLeft for it.
func linkRetryWait(ctx context.Context, minLeft time.Duration) bool {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < minLeft+linkRetryPause {
		return false
	}
	select {
	case <-time.After(linkRetryPause):
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// linkLoginRetryable: a Link login that failed on the network, on time, or on
// a server's 5xx (R1). Authentication, identity, redirect, protocol, and
// subscription failures stay final.
func linkLoginRetryable(err error) bool {
	var cloudErr *CloudError
	if !errors.As(err, &cloudErr) {
		return false
	}
	return cloudErr.Code == CloudErrNetwork || cloudErr.Code == CloudErrTimeout ||
		cloudErr.Retryable && cloudErr.StatusCode >= http.StatusInternalServerError
}

// linkReadRetryable: a session read over Link that got no answer from the
// Relay (R4): no HTTP answer at all, Ingress unavailable, or an answer from
// Link or Home Assistant instead of the Relay. A redirect or any answer from
// the Relay stays final.
func linkReadRetryable(err error) bool {
	var cloudErr *CloudError
	if !errors.As(err, &cloudErr) {
		return false
	}
	return cloudErr.Code == CloudErrIngressUnavailable ||
		cloudErr.Code == CloudErrOutcomeUnknown && (cloudErr.StatusCode == 0 || cloudErr.Retryable)
}

// linkRouteMemory: route automatic found the local Relay unreachable and
// Link verified, for this profile, Relay instance, and local address, until
// Until (at most linkRouteMemoryTTL ahead).
type linkRouteMemory struct {
	ProfileID       string    `json:"profile_id"`
	RelayInstanceID string    `json:"relay_instance_id"`
	RelayURL        string    `json:"relay_url"`
	Until           time.Time `json:"until"`
}

// linkRouteMemoryFile is where the remembered route lives; "" turns it off.
var linkRouteMemoryFile = sync.OnceValue(func() string {
	paths, err := detectPaths()
	if err != nil {
		return ""
	}
	return filepath.Join(paths.CacheDir, "link-route.json")
})

func linkRouteFor(cfg runtimeConfig) (linkRouteMemory, bool) {
	route := linkRouteMemory{ProfileID: cfg.ProfileID, RelayInstanceID: cfg.RelayInstanceID, RelayURL: cfg.RelaySecureBaseURL + " " + cfg.RelayBaseURL}
	return route, route.ProfileID != "" && route.RelayInstanceID != "" && linkRouteMemoryFile() != ""
}

func linkRouteRemembered(cfg runtimeConfig) bool {
	want, ok := linkRouteFor(cfg)
	if !ok {
		return false
	}
	data, err := os.ReadFile(linkRouteMemoryFile())
	var saved linkRouteMemory
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return false
	}
	now := time.Now()
	return saved.ProfileID == want.ProfileID && saved.RelayInstanceID == want.RelayInstanceID && saved.RelayURL == want.RelayURL &&
		now.Before(saved.Until) && !saved.Until.After(now.Add(linkRouteMemoryTTL))
}

// rememberLinkRoute is best effort: without it the next command probes the
// local Relay again, as before.
func rememberLinkRoute(cfg runtimeConfig) {
	route, ok := linkRouteFor(cfg)
	if !ok {
		return
	}
	route.Until = time.Now().Add(linkRouteMemoryTTL)
	data, err := json.Marshal(route)
	if err == nil && os.MkdirAll(filepath.Dir(linkRouteMemoryFile()), 0o700) == nil {
		_ = writeFileAtomic(linkRouteMemoryFile(), data, 0o600)
	}
}

func forgetLinkRoute() {
	if path := linkRouteMemoryFile(); path != "" {
		_ = os.Remove(path)
	}
}
