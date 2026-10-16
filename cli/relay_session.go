package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

// Session budgets (typed-verbs spec §2.2). Variables so tests can shrink them.
var (
	relaySessionRequestTimeout = 15 * time.Second
	relaySessionOverallTimeout = 30 * time.Second
)

const relaySessionMaxConcurrent = 8

// readOnlyWSTypes is the fixed WS allowlist behind invariant I1: read verbs
// can only ever send these message types. Extending it is a reviewed change.
var readOnlyWSTypes = map[string]bool{
	"config/entity_registry/list_for_display": true,
	"config/entity_registry/list":             true,
	"config/entity_registry/get":              true,
	"config/area_registry/list":               true,
	"config/floor_registry/list":              true, // map: floors
	"config/device_registry/list":             true,
	// Pre-2026.8 merged device ids → the devices they were split into.
	"config/device_registry/list_composite_splits": true,
	"search/related": true,
	// Tier engine (PR2): config_entries/get lists entries (consumer-manager
	// presence); blueprint/substitute renders a blueprint with inputs and
	// returns it without storing anything.
	"config_entries/get": true,
	// call/apply reauth correlation (spec §2.3): lists pending config flows.
	"config_entries/flow/progress": true,
	// find alias fallback: get_entries returns extended rows incl. aliases
	// (the list endpoints carry none).
	"config/entity_registry/get_entries": true,
	"blueprint/substitute":               true,
	// The config Home Assistant has LOADED for an automation/script, by
	// entity_id: YAML and package items without a UI config too.
	"automation/config": true,
	"script/config":     true,
}

// relaySession resolves config and transport once per invocation and
// memoizes identical reads. It is the only Relay path of the read verbs and
// refuses anything not provably read-only before sending (I1).
type relaySession struct {
	paths        runtimePaths
	cfg          runtimeConfig
	transport    relayTransportSelection
	server       string
	ctx          context.Context
	cancel       context.CancelFunc
	slots        chan struct{}
	mu           sync.Mutex
	memo         map[string]*relayMemo
	relayVersion string
	answered     bool  // the Relay has answered at least one request
	unreachable  error // transport failure before the Relay ever answered
}

type relayMemo struct {
	done chan struct{}
	data json.RawMessage
	err  error
}

func newRelaySession(paths runtimePaths, opts verbOptions) (*relaySession, error) {
	if opts.serverSet {
		setServerSelectionOverride(opts.server)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		return nil, err
	}
	var via relayVia
	if opts.viaSet {
		via, _ = parseRelayVia(opts.via)
	}
	ctx, cancel := context.WithTimeout(context.Background(), relaySessionOverallTimeout)
	transport, err := selectRelayTransport(ctx, cfg, via, opts.viaSet)
	if err != nil {
		cancel()
		return nil, errors.New(relayTransportErrorMessage(err))
	}
	return &relaySession{
		paths:     paths,
		cfg:       cfg,
		transport: transport,
		server:    activeServerProfile(),
		ctx:       ctx,
		cancel:    cancel,
		slots:     make(chan struct{}, relaySessionMaxConcurrent),
		memo:      map[string]*relayMemo{},
	}, nil
}

func (s *relaySession) close() { s.cancel() }

// coreGET reads one Home Assistant REST path and returns the upstream status
// and body from the Relay envelope.
func (s *relaySession) coreGET(path string) (int, json.RawMessage, error) {
	data, err := s.read("core", map[string]any{"method": http.MethodGet, "path": path})
	if err != nil {
		return 0, nil, err
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &core); err != nil || core.Status == 0 {
		return 0, nil, fmt.Errorf("Relay core response for %s has no upstream status", path)
	}
	return core.Status, core.Body, nil
}

func (s *relaySession) ws(payload map[string]any) (json.RawMessage, error) {
	return s.read("ws", payload)
}

// wsDecode reads one WS result into `into`; what names it in parse errors.
func (s *relaySession) wsDecode(payload map[string]any, into any, what string) error {
	data, err := s.ws(payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("cannot parse %s: %w", what, err)
	}
	return nil
}

// read checks the I1 allowlist, then sends each distinct request at most once
// per session; concurrent identical callers share one in-flight request.
func (s *relaySession) read(endpoint string, payload map[string]any) (json.RawMessage, error) {
	if err := checkReadOnlyRequest(endpoint, payload); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload) // map keys sort: a canonical memo key
	if err != nil {
		return nil, err
	}
	key := endpoint + " " + string(body)
	s.mu.Lock()
	if m, ok := s.memo[key]; ok {
		s.mu.Unlock()
		<-m.done
		return m.data, m.err
	}
	m := &relayMemo{done: make(chan struct{})}
	s.memo[key] = m
	s.mu.Unlock()
	m.data, m.err = s.send(endpoint, body)
	close(m.done)
	return m.data, m.err
}

func checkReadOnlyRequest(endpoint string, payload map[string]any) error {
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		path, _ := payload["path"].(string)
		if method == http.MethodGet && readablePath(path) && len(payload) == 2 {
			return nil
		}
		return fmt.Errorf("read verbs only send core GET /api/...; refused %q %q, nothing was sent", method, path)
	case "ws":
		if wsType, _ := payload["type"].(string); readOnlyWSTypes[wsType] {
			return nil
		}
		return fmt.Errorf("WS type %q is not on the read-only allowlist; nothing was sent", payload["type"])
	case "backups": // plan restore: loading one automation/script snapshot
		if file, _ := payload["file"].(string); payload["action"] == "load" && len(payload) == 2 && restoreDomain(file) != "" {
			return nil
		}
		return errors.New("read verbs only load automation/script config snapshots from relay backups; nothing was sent")
	default:
		return fmt.Errorf("read verbs never use relay %s; nothing was sent", endpoint)
	}
}

// GET surfaces that act or stream rather than read: a webhook GET can fire
// automations, the proxies stream media, backups download archives.
var readDeniedPrefixes = []string{"/api/webhook", "/api/hassio", "/api/camera_proxy", "/api/image_proxy",
	"/api/media_player_proxy", "/api/tts_proxy", "/api/backup", "/api/stream"}

// readablePath checks the decoded, normalized path: under /api/, no dot
// segments or doubled slashes, and not a denied surface.
func readablePath(raw string) bool {
	pathPart, _, _ := strings.Cut(raw, "?")
	decoded, err := url.PathUnescape(pathPart)
	if err != nil || !strings.HasPrefix(decoded, "/api/") || path.Clean(decoded) != decoded {
		return false
	}
	for _, denied := range readDeniedPrefixes {
		if strings.HasPrefix(decoded, denied) {
			return false
		}
	}
	return true
}

func (s *relaySession) send(endpoint string, body []byte) (json.RawMessage, error) {
	return s.sendWithin(endpoint, body, relaySessionRequestTimeout)
}

// sendWithin is send with a caller-chosen per-request bound (verification
// reads must not outlast their window).
func (s *relaySession) sendWithin(endpoint string, body []byte, timeout time.Duration) (json.RawMessage, error) {
	select {
	case s.slots <- struct{}{}:
	case <-s.ctx.Done():
		return nil, fmt.Errorf("HA NOVA read budget of %s exceeded", relaySessionOverallTimeout)
	}
	defer func() { <-s.slots }()
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	req, err := newRelayRequest(ctx, s.transport, endpoint, body)
	if err != nil {
		return nil, err
	}
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		err = sendTransportError(s, endpoint, err)
		s.mu.Lock()
		if !s.answered && s.unreachable == nil {
			s.unreachable = err
		}
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Lock()
	s.answered = true
	s.mu.Unlock()
	defer resp.Body.Close()
	if version := resp.Header.Get(relayVersionHeader); version != "" {
		s.mu.Lock()
		s.relayVersion = version
		s.mu.Unlock()
	}
	if isHTTPRedirect(resp.StatusCode) {
		return nil, fmt.Errorf("Relay answered HTTP %d (redirect); redirects are never followed", resp.StatusCode)
	}
	if s.transport.Via == relayViaLocal &&
		(resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		return nil, errors.New(localRelayAuthRepairMessage(resp.StatusCode, s.server, s.cfg.RelayBaseURL))
	}
	raw, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err == nil {
		raw, err = normalizeUTF8Bytes(raw, "Relay response")
	}
	if err != nil {
		return nil, err
	}
	var envelope struct {
		OK    *bool           `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.OK == nil {
		return nil, fmt.Errorf("Relay %s response (HTTP %d) is not a valid result envelope", endpoint, resp.StatusCode)
	}
	if !*envelope.OK {
		return nil, &relayCallError{endpoint: endpoint, status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	return envelope.Data, nil
}

// sendTransportError words a failure that got no HTTP response at all.
func sendTransportError(s *relaySession, endpoint string, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("Relay %s read timed out (%s per request, %s overall); nothing was changed", endpoint, relaySessionRequestTimeout, relaySessionOverallTimeout)
	case s.transport.Via == relayViaCloud:
		// Cloud round-tripper failures (device rejected, session expired)
		// carry their own sanitized wording; the local "is the App running"
		// hint would point at a fake host.
		return cloudProblemForError(err)
	}
	return errors.New(relayConnectErrorMessage(s.transport.BaseURL, err))
}

// unreachableError is the transport failure of an invocation in which the
// Relay never answered: an operational error (exit 1), not partial data.
func (s *relaySession) unreachableError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.answered {
		return nil
	}
	return s.unreachable
}

// version returns the Relay version seen in any response header, or "".
func (s *relaySession) version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.relayVersion
}

// relayCallError is a Relay result envelope with ok:false. The message keeps
// Home Assistant's own error code (e.g. "not_found") for callers to inspect.
type relayCallError struct {
	endpoint string
	status   int
	code     string
	message  string
}

func (e *relayCallError) Error() string {
	return fmt.Sprintf("relay %s failed (HTTP %d, %s): %s", e.endpoint, e.status, e.code, e.message)
}

// isUpstreamUnknownCommand: Home Assistant does not know the WS command
// (an older core).
func isUpstreamUnknownCommand(err error) bool {
	var call *relayCallError
	return errors.As(err, &call) && strings.Contains(call.message, "unknown_command")
}

func isUpstreamNotFound(err error) bool {
	var call *relayCallError
	return errors.As(err, &call) && strings.Contains(call.message, "not_found")
}
