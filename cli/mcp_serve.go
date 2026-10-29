package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// `ha-nova serve`: the chat-app bridge (docs/work/2026-10-19-chat-bridge-
// contract.md). The same
// tools, instructions, skills, and verbs as `ha-nova mcp`, over MCP
// Streamable HTTP (stateless: one JSON-RPC message per POST, one JSON
// answer; no SSE, no elicitation). Only the Home Assistant integration
// calls it: it adds the bridge secret and the HA user it authenticated.
// Plans, phone links, and the owner's switches live in --data-dir.

const serveUsage = "ha-nova serve --listen <host:port> --data-dir <dir> --secret-file <file> [--confirm-wait 25s] [--rate-limit <n>] [--server <name> | --relay-file <file>] [--control-listen <127.0.0.1:port> --control-secret-file <file>]"

const (
	serveUserHeader = "X-Ha-User-Id"
	serveMaxBody    = 1 << 20
)

var (
	serveSecretPattern = regexp.MustCompile(`^[0-9a-f]{64}$`) // 256 bits, hex
	haUserIDPattern    = regexp.MustCompile(`^[0-9a-f]{32}$`) // Home Assistant user ids (uuid4 hex)
)

// chatBridge is the server: its config, and state shared across requests.
type chatBridge struct {
	paths   runtimePaths // the bridge's own HA NOVA config (its Relay profile)
	dataDir string
	secret  [32]byte // sha256 of the shared secret
	pin     string
	wait    time.Duration // how long one ha_apply waits for a tap
	limiter rateLimiter   // --rate-limit (mcp_serve_limit.go)
	log     *log.Logger

	mu      sync.Mutex
	jobs    map[string]*chatJob        // user/plan → its phone ask (mcp_serve_phone.go)
	told    map[string]map[string]bool // per user: notice hints already given
	owner   string                     // the HA owner's user id, the default allowed user
	ownerAt time.Time
	ownerNo time.Time // the last failed owner read: retried after 5 s

	control     [32]byte               // sha256 of the control secret (mcp_serve_control.go)
	controlJobs map[string]*controlJob // job id → a phone change (under mu)
	linksMu     sync.Mutex             // phone-links.json: the bridge is its only writer
}

func runServeCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	var listen, dataDir, secretFile, relayFile, controlListen, controlSecret string
	wait := 25 * time.Second
	limit := 0
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&opts.server, "server", "", "server profile name (or HA_NOVA_SERVER)")
	fs.StringVar(&listen, "listen", "", "address to listen on, host:port (required; the NOVA App uses an internal port)")
	fs.StringVar(&dataDir, "data-dir", "", "directory for plans, phone links, and chat-apps.json (required)")
	fs.StringVar(&secretFile, "secret-file", "", "file holding the 64-hex-character secret the integration sends (required)")
	fs.StringVar(&relayFile, "relay-file", "", "the NOVA App's internal Relay credential file, in place of a paired server profile")
	fs.DurationVar(&wait, "confirm-wait", wait, "how long one ha_apply waits for a phone tap before it answers waiting_for_phone (1s-2m)")
	fs.IntVar(&limit, "rate-limit", 0, "requests per minute per Home Assistant user, then 429 until the minute is over (0: no limit)")
	fs.StringVar(&controlListen, "control-listen", "", "loopback address for the NOVA App's phone-link control channel (with --control-secret-file)")
	fs.StringVar(&controlSecret, "control-secret-file", "", "file holding the control channel's own 64-hex-character secret")
	positional, err := parseVerbArgs(fs, &opts, args, serveUsage+"\n\nEXPERIMENTAL — not supported yet; the interface may change.\nServes the HA NOVA tools over MCP Streamable HTTP for chat apps, behind the Home Assistant integration.")
	if errors.Is(err, errHelpShown) {
		return 0
	}
	host, _, splitErr := net.SplitHostPort(listen)
	switch {
	case err != nil:
	case len(positional) != 0 || dataDir == "" || secretFile == "":
		err = errors.New("Usage: " + serveUsage)
	case splitErr != nil || host == "":
		err = errors.New("--listen needs an explicit host:port, e.g. 127.0.0.1:8765")
	case wait < time.Second || wait > 2*time.Minute:
		err = errors.New("--confirm-wait must be between 1s and 2m")
	case limit < 0:
		err = errors.New("--rate-limit must be 0 (no limit) or more")
	case (controlListen == "") != (controlSecret == ""):
		err = errors.New("--control-listen and --control-secret-file go together")
	case controlListen != "" && controlAddr(controlListen) != nil:
		err = controlAddr(controlListen)
	case relayFile != "" && opts.serverSet:
		err = errors.New("--relay-file and --server exclude each other")
	case relayFile != "":
		bridgeRelay, err = loadBridgeRelay(relayFile)
	}
	var b *chatBridge
	if err == nil {
		b, err = newChatBridge(paths, dataDir, secretFile, wait, os.Stderr)
	}
	if err == nil && controlSecret != "" {
		err = b.enableControl(controlSecret)
	}
	if err != nil {
		printErr("%s", err)
		return 1
	}
	b.limiter.limit = limit
	if opts.serverSet {
		setServerSelectionOverride(opts.server)
		b.pin = opts.server
		if opts.server == demoProfileName { // the hosted demo's bridge: its plan store goes with every reset
			demoPlansSince = readPlansSince(dataDir)
		}
	}
	// Verbs print to stdout and stderr, which runVerb captures; the log keeps
	// the real stderr.
	os.Stdout = os.Stderr
	reapUpdateRefresh = true
	// As ha-nova mcp, once: the phone jobs read it outside the verb lock.
	phoneTellSpan = 2 * time.Second
	_ = os.Setenv(updateNudgeOptOutEnv, "1") // the App updates the bridge; a chat user has no terminal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := b.listen(ctx, listen, controlListen); err != nil {
		printErr("serve: %s", err)
		return 1
	}
	return 0
}

func newChatBridge(paths runtimePaths, dataDir, secretFile string, wait time.Duration, logTo io.Writer) (*chatBridge, error) {
	secret, err := readServeSecret(secretFile, "secret")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the data directory: %w", err)
	}
	return &chatBridge{paths: paths, dataDir: dataDir, secret: secret, wait: wait,
		log: log.New(logTo, "ha-nova serve: ", log.LstdFlags), jobs: map[string]*chatJob{}, told: map[string]map[string]bool{}}, nil
}

// listen serves /mcp on addr and, with controlAddr, the App's control
// channel on a listener of its own (mcp_serve_control.go). Both bind
// before either serves; both stop with ctx, or when one fails.
func (b *chatBridge) listen(ctx context.Context, addr, controlAddr string) error {
	var servers []*http.Server
	var listeners []net.Listener
	for _, l := range []struct {
		addr    string
		handler http.Handler
	}{{addr, b}, {controlAddr, http.HandlerFunc(b.serveControl)}} {
		if l.addr == "" {
			continue
		}
		listener, err := net.Listen("tcp", l.addr)
		if err != nil {
			for _, open := range listeners {
				_ = open.Close()
			}
			return err
		}
		listeners = append(listeners, listener)
		servers = append(servers, &http.Server{Handler: l.handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
			WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10})
	}
	for _, listener := range listeners {
		b.log.Printf("listening on %s", listener.Addr())
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(servers))
	for i, server := range servers {
		go func() {
			err := server.Serve(listeners[i])
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			cancel()
			errs <- err
		}()
	}
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	for _, server := range servers {
		_ = server.Shutdown(shutdown) // running calls finish first (bounded)
	}
	var first error
	for range servers {
		if err := <-errs; err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ServeHTTP: POST /mcp only. The log line names the method, the tool, the
// HTTP status, and a user id prefix: never a secret, code, argument, or
// result.
func (b *chatBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, status, user, method := time.Now(), http.StatusOK, "-", "-"
	defer func() {
		b.log.Printf("%s %q user=%.8s rpc=%s http=%d %s", r.Method, r.URL.Path, user, method, status, time.Since(start).Round(time.Millisecond))
	}()
	refuse := func(code int, message string) {
		status = code
		http.Error(w, message, code)
	}
	if r.URL.Path != "/mcp" {
		refuse(http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost { // stateless: no SSE stream, no session to delete
		w.Header().Set("Allow", http.MethodPost)
		refuse(http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	call, code, message := b.authorize(r)
	if call == nil && code == http.StatusForbidden { // a signed-in user who may not: words a chat app can show
		status = code
		refuseRPC(w, r, code, message)
		return
	}
	if call == nil {
		refuse(code, message)
		return
	}
	user = call.key
	if !b.limiter.allow(call.key) {
		status = http.StatusTooManyRequests
		refuseRPC(w, r, status, "Too many requests to HA NOVA right now. Wait a minute, then try again.")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, serveMaxBody))
	if err != nil {
		refuse(http.StatusRequestEntityTooLarge, "request body too large or unreadable")
		return
	}
	method = rpcLabel(body)
	userPaths := b.paths
	userPaths.CacheDir = filepath.Join(b.dataDir, "users", call.key) // plans are bound to their user
	if err := os.MkdirAll(userPaths.CacheDir, 0o700); err != nil {
		refuse(http.StatusInternalServerError, "cannot prepare this user's data")
		return
	}
	var out bytes.Buffer
	srv := &mcpServer{paths: userPaths, out: &out, pin: b.pin, noForms: true, told: b.toldFor(call.user), chat: call}
	// Stateless: a legacy client's revision comes from the header (none:
	// the newest legacy one); 2026-07-28 requests carry their own _meta.
	switch version := r.Header.Get("Mcp-Protocol-Version"); {
	case version == "":
		srv.legacy = mcpLegacyVersions[0]
	case slices.Contains(mcpLegacyVersions, version):
		srv.legacy = version
	case version != mcpModernVersion:
		refuse(http.StatusBadRequest, "unsupported MCP-Protocol-Version")
		return
	}
	srv.handle(body)
	if out.Len() == 0 { // a notification or a response: nothing to answer
		status = http.StatusAccepted
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(bytes.TrimRight(out.Bytes(), "\n"))
}

// authorize checks the secret (constant time), the one HA user header, and
// the owner's allowed users, on every request. nil: refused, with why.
func (b *chatBridge) authorize(r *http.Request) (*chatPolicy, int, string) {
	auth := r.Header.Values("Authorization")
	token, bearer := "", false
	if len(auth) == 1 {
		token, bearer = strings.CutPrefix(auth[0], "Bearer ")
	}
	got := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(got[:], b.secret[:]) != 1 || !bearer {
		return nil, http.StatusUnauthorized, "unauthorized"
	}
	users := r.Header.Values(serveUserHeader)
	if len(users) != 1 || !haUserIDPattern.MatchString(users[0]) {
		return nil, http.StatusUnauthorized, "unauthorized: no Home Assistant user"
	}
	return b.policyFor(users[0])
}

// refuseRPC answers a refused request as a JSON-RPC error with the request's
// own id, so a chat app can show message instead of a bare HTTP status.
// Only after the secret and the user header passed (the request came
// through the integration for a signed-in user).
func refuseRPC(w http.ResponseWriter, r *http.Request, code int, message string) {
	var request struct {
		ID json.RawMessage `json:"id"`
	}
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, serveMaxBody))
	id := json.RawMessage("null")
	if json.Unmarshal(body, &request) == nil && len(request.ID) > 0 && (request.ID[0] == '"' || request.ID[0] == '-' || request.ID[0] >= '0' && request.ID[0] <= '9') {
		id = request.ID // a JSON-RPC id is a string or a number
	}
	answer, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32001, "message": message}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(answer)
}

func (b *chatBridge) toldFor(user string) map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.told[user] == nil {
		b.told[user] = map[string]bool{}
	}
	return b.told[user]
}

// rpcLabel names a request for the log: its method, and a tool call's
// tool. Nothing else of the body is read.
func rpcLabel(body []byte) string {
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &msg) != nil || !mcpLogWord.MatchString(msg.Method) {
		return "-"
	}
	if msg.Method == "tools/call" && mcpLogWord.MatchString(msg.Params.Name) {
		return msg.Method + ":" + msg.Params.Name
	}
	return msg.Method
}

var mcpLogWord = regexp.MustCompile(`^[a-z_/]{1,40}$`)

// readPlansSince: the hosted demo's reset time from <data-dir>/plans-since
// (one RFC 3339 line); zero if there is none.
func readPlansSince(dataDir string) time.Time {
	raw, err := os.ReadFile(filepath.Join(dataDir, "plans-since"))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}
	}
	return t
}
