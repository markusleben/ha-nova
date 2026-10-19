package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	"runtime"
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

const serveUsage = "ha-nova serve --listen <host:port> --data-dir <dir> --secret-file <file> [--confirm-wait 25s] [--server <name> | --relay-file <file>]"

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
	log     *log.Logger

	mu      sync.Mutex
	jobs    map[string]*chatJob        // user/plan → its phone ask (mcp_serve_phone.go)
	told    map[string]map[string]bool // per user: notice hints already given
	owner   string                     // the HA owner's user id, the default allowed user
	ownerAt time.Time
	ownerNo time.Time // the last failed owner read: retried after 5 s
}

func runServeCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	var listen, dataDir, secretFile, relayFile string
	wait := 25 * time.Second
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&opts.server, "server", "", "server profile name (or HA_NOVA_SERVER)")
	fs.StringVar(&listen, "listen", "", "address to listen on, host:port (required; the NOVA App uses an internal port)")
	fs.StringVar(&dataDir, "data-dir", "", "directory for plans, phone links, and chat-apps.json (required)")
	fs.StringVar(&secretFile, "secret-file", "", "file holding the 64-hex-character secret the integration sends (required)")
	fs.StringVar(&relayFile, "relay-file", "", "the NOVA App's internal Relay credential file, in place of a paired server profile")
	fs.DurationVar(&wait, "confirm-wait", wait, "how long one ha_apply waits for a phone tap before it answers waiting_for_phone (1s-2m)")
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
	case relayFile != "" && opts.serverSet:
		err = errors.New("--relay-file and --server exclude each other")
	case relayFile != "":
		bridgeRelay, err = loadBridgeRelay(relayFile)
	}
	var b *chatBridge
	if err == nil {
		b, err = newChatBridge(paths, dataDir, secretFile, wait, os.Stderr)
	}
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if opts.serverSet {
		setServerSelectionOverride(opts.server)
		b.pin = opts.server
	}
	// Verbs print to stdout and stderr, which runVerb captures; the log keeps
	// the real stderr.
	os.Stdout = os.Stderr
	reapUpdateRefresh = true
	_ = os.Setenv(updateNudgeOptOutEnv, "1") // the App updates the bridge; a chat user has no terminal
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := b.listen(ctx, listen); err != nil {
		printErr("serve: %s", err)
		return 1
	}
	return 0
}

func newChatBridge(paths runtimePaths, dataDir, secretFile string, wait time.Duration, logTo io.Writer) (*chatBridge, error) {
	raw, err := os.ReadFile(secretFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read the secret file: %w", err)
	}
	if info, err := os.Stat(secretFile); err == nil && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("the secret file must be readable by its owner only (chmod 600)")
	}
	secret := strings.TrimSpace(string(raw))
	if !serveSecretPattern.MatchString(secret) {
		return nil, errors.New("the secret file must hold exactly 64 lowercase hex characters (256 bits)")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the data directory: %w", err)
	}
	return &chatBridge{paths: paths, dataDir: dataDir, secret: sha256.Sum256([]byte(secret)), wait: wait,
		log: log.New(logTo, "ha-nova serve: ", log.LstdFlags), jobs: map[string]*chatJob{}, told: map[string]map[string]bool{}}, nil
}

func (b *chatBridge) listen(ctx context.Context, addr string) error {
	server := &http.Server{Addr: addr, Handler: b, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	b.log.Printf("listening on %s", listener.Addr())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown) // running calls finish first (bounded)
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	return nil
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
	if call == nil {
		refuse(code, message)
		return
	}
	user = call.key
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

// policyFor: the owner's current switches for user, or why they may not
// use chat apps. Read on every request, and again before a phone tap
// applies (mcp_serve_phone.go).
func (b *chatBridge) policyFor(user string) (*chatPolicy, int, string) {
	settings, err := readChatSettings(b.dataDir)
	if err != nil {
		b.log.Printf("refusing every request: %s", err)
		return nil, http.StatusServiceUnavailable, "HA NOVA chat-app settings are unreadable"
	}
	allowed := settings.AllowedUsers
	if allowed == nil {
		owner, err := b.ownerID()
		if err != nil {
			return nil, http.StatusServiceUnavailable, "HA NOVA can't read the Home Assistant owner right now"
		}
		allowed = &[]string{owner}
	}
	if !slices.Contains(*allowed, user) {
		return nil, http.StatusForbidden, "This Home Assistant user may not use HA NOVA in chat apps. The owner can allow it on the HA NOVA page."
	}
	sum := sha256.Sum256([]byte("ha-nova/chat-user/v1\x00" + user))
	return &chatPolicy{user: user, key: hex.EncodeToString(sum[:16]), links: filepath.Join(b.dataDir, "phone-links.json"), codes: settings.ChatCodes == nil || *settings.ChatCodes,
		readOnly: settings.ReadOnly, bridge: b}, 0, ""
}

// chatSettings is chat-apps.json, the owner's switches (written by the
// NOVA App). Missing: the defaults (the owner only, changes allowed, chat
// codes on). Unreadable or unknown fields: every request is refused.
type chatSettings struct {
	AllowedUsers *[]string `json:"allowed_users"` // absent: the owner only
	ReadOnly     bool      `json:"read_only"`
	ChatCodes    *bool     `json:"chat_codes"` // absent: on
}

func readChatSettings(dataDir string) (chatSettings, error) {
	var settings chatSettings
	raw, err := os.ReadFile(filepath.Join(dataDir, "chat-apps.json"))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return settings, fmt.Errorf("chat-apps.json: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF { // trailing data: never a partly read file
		return settings, errors.New("chat-apps.json: trailing data")
	}
	return settings, nil
}

// ownerID reads the HA owner through the Relay, cached for a minute.
func (b *chatBridge) ownerID() (string, error) {
	cached := func() (string, error, bool) {
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case b.owner != "" && time.Since(b.ownerAt) < time.Minute:
			return b.owner, nil, true
		case time.Since(b.ownerNo) < 5*time.Second: // a failing Relay is not asked again per request
			return "", errors.New("the owner could not be read a moment ago"), true
		}
		return "", nil, false
	}
	if owner, err, ok := cached(); ok {
		return owner, err
	}
	mcpVerbLock.Lock() // a Relay session sets process-wide server selection
	defer mcpVerbLock.Unlock()
	if owner, err, ok := cached(); ok { // another request read it while this one waited
		return owner, err
	}
	owner, err := b.readOwner()
	b.mu.Lock()
	if err != nil {
		b.ownerNo = time.Now()
	} else {
		b.owner, b.ownerAt = owner, time.Now()
	}
	b.mu.Unlock()
	return owner, err
}

// readOwner: the active owner from config/auth/list (under mcpVerbLock).
func (b *chatBridge) readOwner() (string, error) {
	override, active := serverSelectionOverride, activeServerProfile()
	defer func() { setServerSelectionOverride(override); setActiveServerProfile(active) }()
	s, err := newRelaySession(b.paths, verbOptions{server: b.pin, serverSet: b.pin != ""})
	if err != nil {
		return "", err
	}
	defer s.close()
	var users []haUser
	if err := s.phoneRead("ws", map[string]any{"type": "config/auth/list"}, &users); err != nil {
		return "", err
	}
	for _, u := range users {
		if u.IsOwner && u.IsActive && !u.SystemGenerated && haUserIDPattern.MatchString(u.ID) {
			return u.ID, nil
		}
	}
	return "", errors.New("no active owner")
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
