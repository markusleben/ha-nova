package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The local setup page server (docs/work/2026-10-18-onboarding-0.27-spec.md
// §6): loopback only, an exact Host check against DNS rebinding, a
// single-use launch token that becomes an HttpOnly SameSite=Strict session
// cookie, state changes only as same-origin JSON POSTs, a strict CSP with
// external scripts only, and no secrets in what it serves. Home Assistant's
// sign-in redirect lands on its /oauth/callback (exact OAuth state, single
// use) and goes on to "/", where the page reads its state with the cookie.

//go:embed setupweb
var setupWebFiles embed.FS

var (
	errSetupPageBadAction = errors.New("setup page: unknown action")
	errSetupPageNotNow    = errors.New("setup page: not waiting for this action")
)

const (
	setupPageCookie  = "ha_nova_setup"
	setupPageMaxBody = 4 << 10
)

// Served from setupweb; anything else is 404.
var setupPageAssets = map[string]string{
	"app.js":          "text/javascript; charset=utf-8",
	"steps.js":        "text/javascript; charset=utf-8",
	"style.css":       "text/css; charset=utf-8",
	"tokens.css":      "text/css; charset=utf-8",
	"logo.svg":        "image/svg+xml",
	"strings.en.json": "application/json; charset=utf-8",
}

type setupPageServer struct {
	ui     *setupWebUI
	server *http.Server
	port   string
	token  string // launch token: the first GET turns it into the session
	used   atomic.Bool
	opened atomic.Bool // the page read its state at least once
	// shownEnd closes once the page read an end screen.
	shownEnd     chan struct{}
	shownEndOnce sync.Once

	mu       sync.Mutex
	session  string
	callback http.Handler // the OAuth callback while a sign-in waits
}

func setupPageRandom() (string, error) {
	buf := make([]byte, 32) // 256 bits
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// startSetupPageServer binds tcp4 127.0.0.1 on a random port; never 0.0.0.0
// or a name.
func startSetupPageServer(ui *setupWebUI) (*setupPageServer, error) {
	token, err := setupPageRandom()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		listener.Close()
		return nil, errors.New("setup page: not bound to 127.0.0.1")
	}
	s := &setupPageServer{ui: ui, port: strconv.Itoa(addr.Port), token: token, shownEnd: make(chan struct{})}
	ui.server = s
	s.server = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
		ErrorLog:          log.New(io.Discard, "", 0), // never logs a URL, token, or cookie
	}
	go func() { _ = s.server.Serve(listener) }()
	return s, nil
}

func (s *setupPageServer) launchURL() string {
	return "http://127.0.0.1:" + s.port + "/s/" + s.token
}

func (s *setupPageServer) close() {
	_ = s.server.Close()
}

// callbackHost, routeCallback, and callbackDone let the Home Assistant
// sign-in use this server (oauthCallbackTab).
func (s *setupPageServer) callbackHost() string { return "127.0.0.1:" + s.port }

func (s *setupPageServer) routeCallback(callback http.Handler) func() {
	s.mu.Lock()
	s.callback = callback
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.callback = nil
		s.mu.Unlock()
	}
}

func (s *setupPageServer) callbackDone(w http.ResponseWriter, _ *http.Request) {
	setupPageBounce(w, "/")
}

// setupPageBounce answers a navigation that came from elsewhere (the launch
// file, Home Assistant's redirect) with a page that moves on to "/" itself:
// that hop starts on this origin, so the SameSite=Strict cookie goes along
// in every browser.
func setupPageBounce(w http.ResponseWriter, target string) {
	setSetupPageHeaders(w.Header()) // the OAuth handler set its own CSP first
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=`+target+`"><title>HA NOVA</title><a href="`+target+`">HA NOVA</a>`)
}

func setSetupPageHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// hostAllowed: the Host header names this server exactly. Anything else is
// another name resolving here (DNS rebinding) and gets 421. Only 127.0.0.1:
// the cookie and the sign-in's redirect address live there.
func (s *setupPageServer) hostAllowed(host string) bool {
	return host == "127.0.0.1:"+s.port
}

func (s *setupPageServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSetupPageHeaders(w.Header())
	if !s.hostAllowed(r.Host) {
		http.Error(w, "Misdirected request", http.StatusMisdirectedRequest)
		return
	}
	if r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	switch p := r.URL.Path; {
	case p == oauthLoopbackCallbackPath:
		s.mu.Lock()
		callback := s.callback
		s.mu.Unlock()
		if callback == nil {
			http.NotFound(w, r)
			return
		}
		callback.ServeHTTP(w, r)
	case strings.HasPrefix(p, "/s/"):
		s.launch(w, r, strings.TrimPrefix(p, "/s/"))
	case p == "/":
		s.static(w, r, "index.html", "text/html; charset=utf-8")
	case strings.HasPrefix(p, "/assets/"):
		name := strings.TrimPrefix(p, "/assets/")
		contentType, ok := setupPageAssets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.static(w, r, name, contentType)
	case p == "/api/state":
		s.state(w, r)
	case p == "/api/action":
		s.action(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *setupPageServer) static(w http.ResponseWriter, r *http.Request, name, contentType string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := setupWebFiles.ReadFile(path.Join("setupweb", name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

// launch turns the single-use token into the session cookie. A reused token
// gets no cookie: its page says it is already open in another tab.
func (s *setupPageServer) launch(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
		http.NotFound(w, r)
		return
	}
	if s.used.CompareAndSwap(false, true) {
		session, err := setupPageRandom()
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.session = session
		s.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: setupPageCookie, Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		setupPageBounce(w, "/")
		return
	}
	setupPageBounce(w, "/#elsewhere") // a second tab, even in the same browser
}

func (s *setupPageServer) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(setupPageCookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	return session != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(session)) == 1
}

// sameOrigin: fetch metadata, when the browser sends it, says this page
// itself made the request.
func sameOrigin(r *http.Request) bool {
	site := r.Header.Get("Sec-Fetch-Site")
	return site == "" || site == "same-origin"
}

func (s *setupPageServer) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if !s.authenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	s.opened.Store(true)
	state := s.ui.snapshot()
	w.Header().Set("Content-Type", "application/json")
	if json.NewEncoder(w).Encode(state) == nil && setupPageEnded(state.Screen) {
		s.shownEndOnce.Do(func() { close(s.shownEnd) })
	}
}

// setupPageEnded: screens after which setup no longer runs.
func setupPageEnded(screen string) bool {
	switch screen {
	case "done", "closed", "incomplete", "finished", "failed", "stopped", "idle":
		return true
	}
	return false
}

// action takes every state change: JSON only (a form or a no-CORS fetch can't
// send it), the exact Origin of this page, the session cookie, and an
// allowlisted action the engine waits for.
func (s *setupPageServer) action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		http.Error(w, "Unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	if r.Header.Get("Origin") != "http://"+r.Host || !sameOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if !s.authenticated(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var body setupPageAction
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, setupPageMaxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || decoder.More() {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	switch err := s.ui.act(body); {
	case errors.Is(err, errSetupPageBadAction):
		http.Error(w, "Bad request", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "Conflict", http.StatusConflict)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeSetupPageLauncher writes a private file that sends the browser to the
// launch URL, so the token never shows in a process list other users can read
// (the browser command line on Linux); the terminal still prints the URL. The
// folder sits in the home folder, not /tmp: snap and Flatpak browsers have a
// /tmp of their own.
func writeSetupPageLauncher(launch, parent string) (string, func(), error) {
	dir, err := os.MkdirTemp(parent, "ha-nova-setup-")
	if err != nil {
		return "", nil, err
	}
	remove := func() { _ = os.RemoveAll(dir) }
	file := filepath.Join(dir, "setup.html")
	page := `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=` + html.EscapeString(launch) + `"><title>HA NOVA</title>`
	if err := os.WriteFile(file, []byte(page), 0o600); err != nil {
		remove()
		return "", nil, err
	}
	return file, remove, nil
}

// openSetupPage opens the launch URL. On Linux other users can read a
// process's command line, so the browser gets a private launcher file there
// instead (WSL's browser runs on Windows and can't read Linux paths). macOS
// and Windows keep other users' command lines to themselves. remove deletes
// the launcher once setup ends.
func openSetupPage(launch string) (remove func(), err error) {
	home, homeErr := os.UserHomeDir()
	if runtime.GOOS != "linux" || os.Getenv("WSL_DISTRO_NAME") != "" || homeErr != nil {
		return func() {}, openBrowserForSetup(launch)
	}
	file, remove, err := writeSetupPageLauncher(launch, home)
	if err != nil {
		return func() {}, err
	}
	return remove, openBrowserForSetup(file)
}
