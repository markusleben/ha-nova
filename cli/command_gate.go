package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// `ha-nova gate`: the NOVA App's safety-check worker
// (docs/work/2026-11-03-relay-typed-gate-spec.md §2 and Contract). The
// Relay asks it, on loopback with a bearer secret, how to treat each request
// a device sends (/check) and to hide secrets in each answer (/redact). It
// decides nothing of its own: gateVerdict and redactRawResponse are the
// CLI's. It reaches Home Assistant only through the Relay, as its own
// internal principal (--relay-file).

const gateUsage = "ha-nova gate --socket <file> --secret-file <file> --relay-file <file>"

// gateCheckBudget bounds one check's reads; the Relay waits a little longer
// (gate-worker.ts). A check the budget cuts stays typed (LIMIT_CUT).
var gateCheckBudget = 15 * time.Second

const gateBodyLimit = 96 << 20 // a base64 answer of the largest registry list

var gatePictureToken = regexp.MustCompile(`[?&]token=([^&#]+)`)

var gateRelayToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`) // the standalone Relay's token for its worker (start.ts)

func runGateCommand(paths runtimePaths, args []string) int {
	var socket, secretFile, relayFile string
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	fs.StringVar(&socket, "socket", "", "Unix socket to listen on, in a folder only its owner can open (required)")
	fs.StringVar(&secretFile, "secret-file", "", "file holding the 64-hex-character secret the Relay sends (required)")
	fs.StringVar(&relayFile, "relay-file", "", "the Relay's credential file for this worker (required)")
	var opts verbOptions
	positional, err := parseVerbArgs(fs, &opts, args, gateUsage+"\n\nInternal, for the NOVA App. Not for direct use.")
	if errors.Is(err, errHelpShown) {
		return 0
	}
	switch {
	case err != nil:
	case len(positional) != 0 || secretFile == "" || relayFile == "":
		err = errors.New("Usage: " + gateUsage)
	case !filepath.IsAbs(socket) || len(socket) >= 104: // a socket path holds at most 104 bytes (macOS)
		err = errors.New("--socket needs an absolute path shorter than 104 bytes")
	default:
		bridgeRelay, err = loadGateRelay(relayFile)
	}
	checkDeadline = time.Time{} // each check has its own budget (gateCheckBudget), never one from the worker's start
	var secret [32]byte
	if err == nil {
		secret, err = readServeSecret(secretFile, "gate secret")
	}
	if err != nil {
		printErr("%s", err)
		return 1
	}
	listener, err := gateListen(socket)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	server := &http.Server{Handler: gateHandler(paths, secret), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10}
	if err := server.Serve(listener); err != nil {
		printErr("%s", err)
		return 1
	}
	return 0
}

// gateListen listens on socket, in a folder only its owner can open (the
// Relay's /data/gate), so no other listener can answer the Relay in the
// worker's place. A socket left by a worker that stopped is replaced; a live
// one, or anything else at that path, is not.
func gateListen(socket string) (net.Listener, error) {
	dir, err := os.Lstat(filepath.Dir(socket))
	if err != nil || !dir.IsDir() || runtime.GOOS != "windows" && dir.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("the socket's folder must be a real folder only its owner can open (chmod 700)")
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("something other than a socket is at " + socket)
		}
		if conn, err := net.Dial("unix", socket); err == nil {
			conn.Close()
			return nil, errors.New("another worker already answers at " + socket)
		}
		if err := os.Remove(socket); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// loadGateRelay: the App's pinned TLS listener with the worker's device
// credential (the bridge's file format), or the standalone Relay's plain
// loopback port with its random token.
func loadGateRelay(file string) (*runtimeConfig, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the relay file: %w", err)
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("the relay file must be a regular file readable by its owner only (chmod 600)")
	}
	if cfg, err := loadBridgeRelay(file); err == nil {
		return cfg, nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the relay file: %w", err)
	}
	var relay struct {
		RelayURL   string `json:"relay_url"`
		Credential string `json:"credential"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&relay); err != nil {
		return nil, fmt.Errorf("the relay file is not valid: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("the relay file is not valid: trailing data")
	}
	u, err := url.Parse(relay.RelayURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || !gateRelayToken.MatchString(relay.Credential) {
		return nil, errors.New("the relay file needs https://host:port with spki_pin and a device credential, or http://127.0.0.1:port with the Relay's token")
	}
	return &runtimeConfig{RelayBaseURL: relay.RelayURL, RoutePolicy: routePolicyLocal, bridgeCredential: relay.Credential}, nil
}

func gateHandler(paths runtimePaths, secret [32]byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Values("Authorization")
		token, bearer := "", false
		if len(auth) == 1 {
			token, bearer = strings.CutPrefix(auth[0], "Bearer ")
		}
		got := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(got[:], secret[:]) != 1 || !bearer {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || !slices.Contains([]string{"/check", "/redact", "/ask", "/phone"}, r.URL.Path) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Path == "/ask" || r.URL.Path == "/phone" {
			gateAskHandler(paths, w, r)
			return
		}
		var in struct {
			Principal json.RawMessage `json:"principal"`
			Endpoint  string          `json:"endpoint"`
			Body      []byte          `json:"body_b64"`
			Request   []byte          `json:"request_b64"`
			Response  []byte          `json:"response_b64"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, gateBodyLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || in.Endpoint == "" {
			http.Error(w, "the request must be one JSON object with endpoint", http.StatusBadRequest)
			return
		}
		var out any
		var err error
		if r.URL.Path == "/check" {
			out, err = gateCheckRequest(paths, in.Endpoint, in.Body)
		} else {
			out, err = gateRedact(paths, in.Endpoint, in.Request, in.Response)
		}
		if err != nil { // the Relay answers 503 (nothing forwarded) or 502 (the answer withheld)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

// gateCheckRequest rates one request on a fresh session (Contract: no memo
// from an earlier check). A session that doesn't open is unavailable.
func gateCheckRequest(paths runtimePaths, endpoint string, body []byte) (gateCheck, error) {
	var s *relaySession
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	return gateVerdict(endpoint, body, func() (*relaySession, error) {
		if s != nil {
			return s, nil
		}
		opened, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
		s = opened
		return opened, err
	})
}

// gateRedact hides secrets in the Relay's whole answer envelope for endpoint
// (redactRawResponse, as `ha-nova relay` does). A rendered template can't be
// redacted by key, so an answer to POST /api/template that holds a live
// camera or image access token is withheld (spec §2).
func gateRedact(paths runtimePaths, endpoint string, request, response []byte) (any, error) {
	if !json.Valid(response) {
		return nil, errors.New("the answer is not JSON")
	}
	payload, ok := decodeObject(request)
	raw, _ := payload["path"].(string)
	target, plain := relayCorePath(raw)
	if !ok || payload == nil || endpoint == "core" && !plain { // the template check needs the request
		return nil, errors.New("the request is not one JSON object with a readable path")
	}
	redacted := redactRawResponse(endpoint, response)
	if endpoint == "core" && strings.EqualFold(target, "/api/template") {
		tokens, err := gateAccessTokens(paths)
		if err != nil {
			return nil, err
		}
		for _, token := range tokens {
			if bytes.Contains(response, []byte(token)) {
				return nil, errors.New("a rendered template holds a camera or image access token")
			}
		}
	}
	return map[string][]byte{"response_b64": redacted}, nil
}

// gateAccessTokens: the live access tokens of every camera and image entity.
func gateAccessTokens(paths runtimePaths) ([]string, error) {
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		return nil, err
	}
	defer s.close()
	req, err := newRelayRequest(s.ctx, s.transport, "core", []byte(`{"method":"GET","path":"/api/states"}`))
	var resp *http.Response
	if err == nil {
		resp, err = s.transport.Client.Do(req) // not s.read: every session read hides the tokens this compares
	}
	var core struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"data"`
	}
	if err == nil {
		defer resp.Body.Close()
		err = json.NewDecoder(io.LimitReader(resp.Body, maxRelayResponseBytes)).Decode(&core)
	}
	if err == nil && (!core.OK || core.Data.Status != http.StatusOK) {
		err = fmt.Errorf("the Relay answered HTTP %d (Home Assistant %d) for the states", resp.StatusCode, core.Data.Status)
	}
	var states []struct {
		EntityID   string `json:"entity_id"`
		Attributes struct {
			AccessToken   string `json:"access_token"`
			EntityPicture string `json:"entity_picture"`
		} `json:"attributes"`
	}
	if err == nil {
		err = json.Unmarshal(core.Data.Body, &states)
	}
	if err == nil && states == nil { // no list is a failed read, not a home without cameras
		err = errors.New("the states are not a list")
	}
	if err != nil {
		return nil, err
	}
	var tokens []string
	for _, st := range states {
		if !strings.HasPrefix(st.EntityID, "camera.") && !strings.HasPrefix(st.EntityID, "image.") {
			continue
		}
		var pictureToken string
		if m := gatePictureToken.FindStringSubmatch(st.Attributes.EntityPicture); m != nil {
			pictureToken = m[1]
		}
		for _, token := range []string{st.Attributes.AccessToken, pictureToken} { // the picture's token opens it as well
			if len(token) >= 16 {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens, nil
}
