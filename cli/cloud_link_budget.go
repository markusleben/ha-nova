package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The Home Assistant Link route's budgets, its one bounded retry, and the
// remembered route (docs/work/2026-11-03-link-route-timeouts-spec.md).
const (
	linkConnectTimeout      = 5 * time.Second // one TCP plus TLS connection; measured 0.1–0.3 s
	linkLoginRetryMinLeft   = 3 * time.Second // a second login needs about this much
	linkRequestRetryMinLeft = time.Second
	linkRouteMemoryTTL      = 2 * time.Minute
	linkRouteMemoryName     = "link-route.json" // in the cache directory; uninstall removes it
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
	transport.MaxIdleConnsPerHost = linkSessionMaxConcurrent
	transport.TLSHandshakeTimeout = linkConnectTimeout // through a proxy; DialTLSContext covers the rest
	bounded := func(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, relayConnectBudget(ctx, linkConnectTimeout))
			defer cancel()
			connection, err := dial(dialCtx, network, address)
			noteLinkSetupFailure(ctx, err)
			return connection, err
		}
	}
	transport.DialContext = bounded((&net.Dialer{}).DialContext)
	transport.DialTLSContext = bounded((&tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}}).DialContext)
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
	return (cloudErr.Code == CloudErrNetwork || cloudErr.Code == CloudErrTimeout) && cloudErr.StatusCode == 0 ||
		cloudErr.Retryable && cloudErr.StatusCode >= http.StatusInternalServerError
}

// linkNoRelayAnswer: a Link request that got no answer from the Relay: no
// HTTP answer at all, Ingress unavailable, or a 502–504 from Link or Home
// Assistant. A GET (R3) and a session read (R4) may go once more; a redirect
// or any answer from the Relay stays final.
func linkNoRelayAnswer(err error) bool {
	var cloudErr *CloudError
	if !errors.As(err, &cloudErr) {
		return false
	}
	switch cloudErr.Code {
	case CloudErrIngressUnavailable:
		return true
	case CloudErrNetwork, CloudErrTimeout, CloudErrOutcomeUnknown:
		return cloudErr.StatusCode == 0 || cloudErr.gatewayFailed
	}
	return false
}

// readNoRelayAnswer: a session read that got no answer from the Relay (R4).
// On the local route only a connection refused, reset, or closed before the
// answer: a port forward with a small accept backlog (a VM's NAT) refuses or
// drops some of a session's parallel connections. A timeout or any TLS
// failure (an error alert, a bad certificate, a wrong pin) stays final.
func readNoRelayAnswer(via relayVia, err error) bool {
	if via == relayViaCloud {
		return linkNoRelayAnswer(err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return !opErr.Timeout() && (opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write")
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// linkRouteMemory: route automatic found the local Relay of Key (profile,
// Relay instance, local addresses) unreachable and Link verified, until Until
// (at most linkRouteMemoryTTL ahead).
type linkRouteMemory struct {
	Key   string    `json:"key"`
	Until time.Time `json:"until"`
}

// linkRouteMemoryFile is where the remembered route lives; "" turns it off.
var linkRouteMemoryFile = sync.OnceValue(func() string {
	paths, err := detectPaths()
	if err != nil {
		return ""
	}
	return filepath.Join(paths.CacheDir, linkRouteMemoryName)
})

func linkRouteKey(cfg runtimeConfig) string {
	if cfg.ProfileID == "" || cfg.RelayInstanceID == "" || linkRouteMemoryFile() == "" {
		return ""
	}
	return strings.Join([]string{cfg.ProfileID, cfg.RelayInstanceID, cfg.RelaySecureBaseURL, cfg.RelayBaseURL}, " ")
}

func linkRouteRemembered(cfg runtimeConfig) bool {
	key := linkRouteKey(cfg)
	if key == "" {
		return false
	}
	data, err := os.ReadFile(linkRouteMemoryFile())
	var saved linkRouteMemory
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return false
	}
	now := time.Now()
	return saved.Key == key && now.Before(saved.Until) && !saved.Until.After(now.Add(linkRouteMemoryTTL))
}

// rememberLinkRoute is best effort: without it the next command probes the
// local Relay again, as before.
func rememberLinkRoute(cfg runtimeConfig) {
	key := linkRouteKey(cfg)
	data, err := json.Marshal(linkRouteMemory{Key: key, Until: time.Now().Add(linkRouteMemoryTTL)})
	if key != "" && err == nil && os.MkdirAll(filepath.Dir(linkRouteMemoryFile()), 0o700) == nil {
		_ = writeFileAtomic(linkRouteMemoryFile(), data, 0o600)
	}
}

func forgetLinkRoute() {
	if path := linkRouteMemoryFile(); path != "" {
		_ = os.Remove(path)
	}
}
