package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRelayConnectTimeoutSeconds = 5
	defaultRelayMaxTimeSeconds        = 15
	// Mirrors the relay's own WS/REST response ceiling (256 MiB).
	maxRelayResponseBytes = 256 << 20
	relayVersionHeader    = "X-Ha-Nova-Relay-Version"
)

var httpClient = newRelayHTTPClient(defaultRelayConnectTimeoutSeconds, defaultRelayMaxTimeSeconds)

func newRelayHTTPClient(connectTimeoutSeconds, maxTimeSeconds float64) *http.Client {
	return &http.Client{
		Timeout: time.Duration(maxTimeSeconds * float64(time.Second)),
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: time.Duration(connectTimeoutSeconds * float64(time.Second))}).DialContext,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// applyHealthTimeouts rewrites a client's dial + overall timeout in place, so the
// paired (SPKI-pinned) transport honors the health command's explicit timeout
// flags instead of the fixed pairing timeout baked into spkiPinnedClient. The
// TLS pin configuration on the existing transport is preserved.
func applyHealthTimeouts(client *http.Client, connectTimeoutSeconds, maxTimeSeconds float64) {
	client.Timeout = time.Duration(maxTimeSeconds * float64(time.Second))
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.DialContext = (&net.Dialer{Timeout: time.Duration(connectTimeoutSeconds * float64(time.Second))}).DialContext
	}
}

type relayRequestOptions struct {
	InlineJSON    string
	JSONFile      string
	InlineJSONSet bool
	JSONFileSet   bool
	JQFilter      string
	JQFile        string
	JQFilterSet   bool
	JQFileSet     bool
	OutputFile    string
	BinaryOutFile string
	OutputFileSet bool
	BinaryOutSet  bool
	Method        string
	Path          string
	StrictStatus  bool
	Server        string
	ServerSet     bool
	Via           string
	ViaSet        bool
}

func runRelayCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova relay <health|ws|core|files|backups|jq|version> ...")
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Println("Usage: ha-nova relay <health|ws|core|files|backups|jq|version> ...")
		fmt.Println("Run 'ha-nova relay <subcommand> --help' to see that subcommand's flags.")
		return 0
	}
	switch args[0] {
	case "health":
		return runHealth(paths, args[1:])
	case "ws":
		return runRelayProxy(paths, "ws", args[1:])
	case "core":
		return runRelayProxy(paths, "core", args[1:])
	case "files":
		return runRelayProxy(paths, "files", args[1:])
	case "backups":
		return runRelayProxy(paths, "backups", args[1:])
	case "jq":
		return runJQ(args[1:])
	case "version":
		if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
			fmt.Println("Usage: ha-nova relay version")
			fmt.Println("Prints the installed skill/CLI version the relay contract is pinned to. No flags.")
			return 0
		}
		fmt.Fprintln(os.Stdout, localVersion(paths))
		return 0
	default:
		printErr("Unknown relay command: %s", args[0])
		return 1
	}
}

func parseRelayFlags(command string, args []string) (relayRequestOptions, error) {
	fs := flag.NewFlagSet("relay "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := relayRequestOptions{}
	switch command {
	case "ws", "files", "backups":
		fs.StringVar(&opts.InlineJSON, "data", "", "inline JSON payload")
		fs.StringVar(&opts.InlineJSON, "d", "", "inline JSON payload")
		fs.StringVar(&opts.JSONFile, "data-file", "", "path to JSON payload file")
	case "core":
		fs.StringVar(&opts.Method, "method", "", "HTTP method")
		fs.StringVar(&opts.Path, "path", "", "core API path")
		fs.StringVar(&opts.InlineJSON, "body", "", "inline JSON body")
		fs.StringVar(&opts.InlineJSON, "d", "", "inline JSON body")
		fs.StringVar(&opts.JSONFile, "body-file", "", "path to JSON body file")
		fs.BoolVar(&opts.StrictStatus, "strict-status", false, "exit nonzero for any upstream HTTP error status")
		fs.StringVar(&opts.BinaryOutFile, "out-binary", "", "decode a base64 upstream body and write the raw bytes to this file")
	default:
		return opts, fmt.Errorf("unsupported relay command: %s", command)
	}
	fs.StringVar(&opts.JQFilter, "jq", "", "jq filter")
	fs.StringVar(&opts.JQFile, "jq-file", "", "path to jq filter file")
	fs.StringVar(&opts.OutputFile, "out", "", "write command output to file")
	fs.StringVar(&opts.Server, "server", "", "server profile name (multi-server installs)")
	fs.StringVar(&opts.Via, "via", "", "relay transport override: local or cloud")

	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova relay "+command+" [flags]") {
			return opts, errHelpShown
		}
		return opts, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "data", "body", "d":
			opts.InlineJSONSet = true
		case "data-file", "body-file":
			opts.JSONFileSet = true
		case "jq":
			opts.JQFilterSet = true
		case "jq-file":
			opts.JQFileSet = true
		case "out":
			opts.OutputFileSet = true
		case "out-binary":
			opts.BinaryOutSet = true
		case "server":
			opts.ServerSet = true
		case "via":
			opts.ViaSet = true
		}
	})
	if fs.NArg() != 0 {
		return opts, fmt.Errorf("relay %s does not accept positional arguments; nothing was sent", command)
	}
	if command == "core" {
		if opts.Method == "" || opts.Path == "" {
			return opts, errors.New("--method and --path are required for relay core")
		}
	}
	if opts.OutputFileSet && strings.TrimSpace(opts.OutputFile) == "" {
		return opts, errors.New("--out requires a non-empty path; nothing was sent")
	}
	if opts.BinaryOutSet && strings.TrimSpace(opts.BinaryOutFile) == "" {
		return opts, errors.New("--out-binary requires a non-empty path; nothing was sent")
	}
	if opts.BinaryOutSet {
		if opts.JQFilterSet || opts.JQFileSet {
			return opts, errors.New("--out-binary cannot be combined with --jq/--jq-file: the binary body is decoded from the raw envelope")
		}
		if opts.OutputFileSet {
			return opts, errors.New("use either --out or --out-binary, not both")
		}
	}
	if opts.ServerSet && strings.TrimSpace(opts.Server) == "" {
		return opts, errors.New("--server requires a non-empty profile name; nothing was sent")
	}
	if opts.ViaSet {
		if _, err := parseRelayVia(opts.Via); err != nil {
			return opts, fmt.Errorf("%w; nothing was sent", err)
		}
	}
	return opts, nil
}

func loadRelayPayload(opts relayRequestOptions) ([]byte, error) {
	inlineSelected := opts.InlineJSONSet || opts.InlineJSON != ""
	fileSelected := opts.JSONFileSet || opts.JSONFile != ""
	switch {
	case inlineSelected && fileSelected:
		return nil, errors.New("use either inline JSON or a file, not both")
	case fileSelected:
		if strings.TrimSpace(opts.JSONFile) == "" {
			return nil, errors.New("JSON payload file flag requires a non-empty path; nothing was sent")
		}
		path := filepath.Clean(opts.JSONFile)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read JSON payload file %q; nothing was sent: %w", opts.JSONFile, err)
		}
		source := fmt.Sprintf("JSON payload file %q", opts.JSONFile)
		data, err = normalizeUTF8Bytes(data, source)
		if err != nil {
			return nil, relayTextFileEncodingError(err)
		}
		if !json.Valid(data) {
			return nil, fmt.Errorf("%s is not valid JSON; nothing was sent", source)
		}
		return data, nil
	case inlineSelected:
		payload := normalizeInlineJSON(opts.InlineJSON)
		payloadBytes, err := normalizeUTF8Bytes([]byte(payload), "inline JSON payload")
		if err != nil {
			return nil, fmt.Errorf("%w; nothing was sent", err)
		}
		if !json.Valid(payloadBytes) {
			return nil, errors.New("inline JSON payload is not valid JSON; nothing was sent. On PowerShell prefer --data-file if quoting rewrites the payload")
		}
		return payloadBytes, nil
	default:
		return nil, nil
	}
}

func buildRelayRequestBody(endpoint string, opts relayRequestOptions, payload []byte) ([]byte, error) {
	if endpoint != "core" {
		return payload, nil
	}
	if err := validateUTF8String(opts.Method, "core method"); err != nil {
		return nil, fmt.Errorf("%w; nothing was sent", err)
	}
	if err := validateUTF8String(opts.Path, "core path"); err != nil {
		return nil, fmt.Errorf("%w; nothing was sent", err)
	}
	prefix, err := json.Marshal(struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}{Method: strings.ToUpper(opts.Method), Path: opts.Path})
	if err != nil {
		return nil, fmt.Errorf("cannot build Relay core request; nothing was sent: %w", err)
	}
	requestBody := append([]byte{}, prefix[:len(prefix)-1]...)
	if len(payload) > 0 {
		requestBody = append(requestBody, []byte(`,"body":`)...)
		requestBody = append(requestBody, payload...)
	}
	requestBody = append(requestBody, '}')
	if _, err := strictJSONBytes(requestBody, "Relay core request"); err != nil {
		return nil, fmt.Errorf("%w; nothing was sent", err)
	}
	return requestBody, nil
}

func normalizeInlineJSON(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 {
		if (trimmed[0] == '\'' || trimmed[0] == '"') && trimmed[0] == trimmed[len(trimmed)-1] {
			inner := trimmed[1 : len(trimmed)-1]
			if json.Valid([]byte(inner)) {
				return inner
			}
		}
	}
	return trimmed
}

func loadJQFilter(opts relayRequestOptions) (string, error) {
	filterSelected := opts.JQFilterSet || opts.JQFilter != ""
	fileSelected := opts.JQFileSet || opts.JQFile != ""
	switch {
	case filterSelected && fileSelected:
		return "", errors.New("use either --jq or --jq-file, not both")
	case fileSelected:
		if strings.TrimSpace(opts.JQFile) == "" {
			return "", errors.New("--jq-file requires a non-empty path; nothing was sent")
		}
		data, err := os.ReadFile(filepath.Clean(opts.JQFile))
		if err != nil {
			return "", fmt.Errorf("cannot read jq filter file %q; nothing was sent: %w", opts.JQFile, err)
		}
		data, err = normalizeUTF8Bytes(data, fmt.Sprintf("jq filter file %q", opts.JQFile))
		if err != nil {
			return "", relayTextFileEncodingError(err)
		}
		filter := strings.TrimSpace(string(data))
		if filter == "" {
			return "", fmt.Errorf("jq filter file %q is empty; nothing was sent", opts.JQFile)
		}
		return filter, nil
	case filterSelected:
		if strings.TrimSpace(opts.JQFilter) == "" {
			return "", errors.New("--jq requires a non-empty filter; nothing was sent")
		}
		return opts.JQFilter, nil
	default:
		return "", nil
	}
}

func runRelayProxy(paths runtimePaths, endpoint string, args []string) int {
	// Parse (and answer --help) before the config/token preflight: help must
	// work on a fresh install where neither exists yet.
	opts, err := parseRelayFlags(endpoint, args)
	if err != nil {
		if errors.Is(err, errHelpShown) {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	payloadBytes, err := loadRelayPayload(opts)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	jqFilter, err := loadJQFilter(opts)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	var compiledJQ *jqProgram
	if jqFilter != "" {
		compiledJQ, err = compileJQFilter(jqFilter)
		if err != nil {
			printErr("%s; nothing was sent", err)
			return 1
		}
	}
	requestBody, err := buildRelayRequestBody(endpoint, opts, payloadBytes)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if opts.ServerSet {
		// Selection order: --server > HA_NOVA_SERVER > default_server. An
		// unknown name fails loud in loadConfig with the list of profiles.
		setServerSelectionOverride(opts.Server)
	}
	relayDeadline := time.Now().Add(time.Duration(defaultRelayMaxTimeSeconds * float64(time.Second)))
	relayCtx, cancelRelay := context.WithDeadline(context.Background(), relayDeadline)
	defer cancelRelay()
	// Local old-copy migration runs only after all command input validates, but
	// does not depend on Relay config or keyring availability.
	migratedFirstUse, migrationContended := repairMissingSessionBootstrapWithContention(paths)

	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	var via relayVia
	if opts.ViaSet {
		via, _ = parseRelayVia(opts.Via)
	}
	transport, err := selectRelayTransport(relayCtx, cfg, via, opts.ViaSet)
	if err != nil {
		printErr("%s", relayTransportErrorMessage(err))
		return 1
	}
	baseURL, client, token := transport.BaseURL, transport.Client, transport.Credential
	url := strings.TrimRight(baseURL, "/") + "/" + endpoint
	req, err := http.NewRequestWithContext(
		relayCtx,
		http.MethodPost,
		url,
		bytes.NewReader(requestBody),
	)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		printErr("%s", relayRequestOutcomeUnknownMessage(baseURL, err))
		return 1
	}
	defer resp.Body.Close()

	if isHTTPRedirect(resp.StatusCode) {
		printRelayHTTPOutcomeUnknown(resp.StatusCode)
		return 1
	}
	if notice := checkRelayVersionValue(paths, resp.Header.Get(relayVersionHeader)); !notice.empty() {
		if shouldWarnRelayOutdated(paths) {
			printHumanNotice(notice)
		}
	}
	maybeNudgeSkillUpdate(paths, true)

	bodyBytes, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err != nil {
		printRelayPostRequestError("reading the Relay response", err)
		return 1
	}
	bodyBytes, err = normalizeUTF8Bytes(bodyBytes, "Relay response")
	if err != nil {
		printRelayPostRequestError("validating the Relay response", err)
		return 1
	}
	envelopeValid, taskSucceeded := relayEnvelopeResult(bodyBytes)
	if !envelopeValid {
		printRelayPostRequestError(
			"validating the Relay result envelope",
			errors.New("Relay response did not contain a valid result envelope"),
		)
		return 1
	}
	upstreamExitStatus := 0
	if endpoint == "core" {
		upstreamExitStatus = relayCoreUpstreamExitStatus(bodyBytes, opts.StrictStatus)
		taskSucceeded = taskSucceeded && relayCoreUpstreamTaskSucceeded(bodyBytes)
	}

	if compiledJQ != nil {
		res, err := applyCompiledJQFilter(compiledJQ, bodyBytes, false)
		if err != nil {
			printRelayPostRequestError("jq filtering", err)
			return 1
		}
		bodyBytes = []byte(res.output)
	}

	if opts.BinaryOutFile != "" {
		if err := writeRelayBinaryBody(bodyBytes, opts.BinaryOutFile); err != nil {
			printRelayPostRequestError("processing --out-binary", err)
			return 1
		}
	} else if opts.OutputFile != "" {
		if err := writeRelayTextOutput(opts.OutputFile, bodyBytes); err != nil {
			printRelayPostRequestError("writing --out", err)
			return 1
		}
	} else {
		written, err := os.Stdout.Write(bodyBytes)
		if err == nil && written != len(bodyBytes) {
			err = io.ErrShortWrite
		}
		if err != nil {
			printRelayPostRequestError("writing stdout", err)
			return 1
		}
		if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
			if _, err := fmt.Fprintln(os.Stdout); err != nil {
				printRelayPostRequestError("writing stdout", err)
				return 1
			}
		}
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		printRelayHTTPOutcomeUnknown(resp.StatusCode)
	}
	exitStatus := 0
	if resp.StatusCode >= 400 {
		exitStatus = 1
	} else if upstreamExitStatus != 0 {
		exitStatus = upstreamExitStatus
	}
	if exitStatus == 0 && taskSucceeded {
		finishMigratedFirstUse(paths, migratedFirstUse, migrationContended, relayDeadline)
	}
	return exitStatus
}

func relayEnvelopeOK(body []byte) bool {
	_, ok := relayEnvelopeResult(body)
	return ok
}

func relayEnvelopeResult(body []byte) (bool, bool) {
	var envelope struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.OK == nil {
		return false, false
	}
	return true, *envelope.OK
}

// writeRelayBinaryBody decodes a base64 upstream body (camera frames and other
// binary responses, marked by the relay with body_encoding: "base64") and
// writes the raw bytes. A missing marker is an error, not a silent text write:
// that would hand the caller a file full of JSON instead of an image.
func writeRelayBinaryBody(envelope []byte, path string) error {
	// body is RawMessage on purpose: a text/JSON body is not a string, and
	// decoding must fail with the actionable "use --out" message instead of an
	// unmarshal error about the field type.
	var parsed struct {
		OK   bool `json:"ok"`
		Data struct {
			Body         json.RawMessage `json:"body"`
			BodyEncoding string          `json:"body_encoding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(envelope, &parsed); err != nil {
		return fmt.Errorf("cannot read the relay response as JSON: %w", err)
	}
	if !parsed.OK {
		return errors.New("relay returned an error envelope; run the request without --out-binary to see it")
	}
	if parsed.Data.BodyEncoding != "base64" {
		return errors.New("upstream body is not binary (no base64 marker) — use --out instead of --out-binary")
	}
	var encoded string
	if err := json.Unmarshal(parsed.Data.Body, &encoded); err != nil {
		return fmt.Errorf("binary body is marked base64 but is not a string: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("cannot decode the binary body: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func relayCoreUpstreamTaskSucceeded(body []byte) bool {
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status *int `json:"status"`
		} `json:"data"`
	}
	return json.Unmarshal(body, &envelope) == nil &&
		envelope.OK &&
		envelope.Data.Status != nil &&
		*envelope.Data.Status >= 200 &&
		*envelope.Data.Status < 400
}

func relayCoreUpstreamExitStatus(body []byte, strict bool) int {
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK {
		return 0
	}
	if envelope.Data.Status >= 500 {
		return 1
	}
	if strict && envelope.Data.Status >= 400 {
		return 1
	}
	return 0
}

type healthOptions struct {
	ConnectTimeoutSeconds float64
	MaxTimeSeconds        float64
	Server                string
	ServerSet             bool
	Via                   string
	ViaSet                bool
}

// parseHealthFlags accepts curl-compatible flag names so callers (session-start
// hook) can bound the probe; previously these args were silently discarded and
// the default 5s dial / 15s total timeouts blocked synchronous hooks on a dead
// relay.
func parseHealthFlags(args []string) (healthOptions, error) {
	fs := flag.NewFlagSet("relay health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := healthOptions{}
	fs.Float64Var(&opts.ConnectTimeoutSeconds, "connect-timeout", defaultRelayConnectTimeoutSeconds, "connection timeout in seconds")
	fs.Float64Var(&opts.MaxTimeSeconds, "max-time", defaultRelayMaxTimeSeconds, "total request timeout in seconds")
	fs.StringVar(&opts.Server, "server", "", "server profile name (multi-server installs)")
	fs.StringVar(&opts.Via, "via", "", "relay transport override: local or cloud")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova relay health [--connect-timeout <s>] [--max-time <s>] [--via <local|cloud>]") {
			return opts, errHelpShown
		}
		return opts, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "server":
			opts.ServerSet = true
		case "via":
			opts.ViaSet = true
		}
	})
	if fs.NArg() != 0 {
		return opts, errors.New("relay health does not accept positional arguments")
	}
	if opts.ServerSet && strings.TrimSpace(opts.Server) == "" {
		return opts, errors.New("--server requires a non-empty profile name")
	}
	if opts.ViaSet {
		if _, err := parseRelayVia(opts.Via); err != nil {
			return opts, err
		}
	}
	if opts.ConnectTimeoutSeconds <= 0 || opts.MaxTimeSeconds <= 0 {
		return opts, errors.New("--connect-timeout and --max-time must be positive seconds")
	}
	return opts, nil
}

func runHealth(paths runtimePaths, args []string) int {
	healthOpts, err := parseHealthFlags(args)
	if err != nil {
		if errors.Is(err, errHelpShown) {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	healthDeadline := time.Now().Add(time.Duration(healthOpts.MaxTimeSeconds * float64(time.Second)))
	healthCtx, cancelHealth := context.WithDeadline(context.Background(), healthDeadline)
	defer cancelHealth()
	if healthOpts.ServerSet {
		setServerSelectionOverride(healthOpts.Server)
	}
	// Local old-copy migration runs only after all command input validates, but
	// does not depend on Relay config or keyring availability.
	repairMissingSessionBootstrap(paths)

	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	var via relayVia
	if healthOpts.ViaSet {
		via, _ = parseRelayVia(healthOpts.Via)
	}
	transport, err := selectRelayTransport(healthCtx, cfg, via, healthOpts.ViaSet)
	if err != nil {
		printErr("%s", relayTransportErrorMessage(err))
		return 1
	}
	baseURL, transportClient, token, deviceMode := transport.BaseURL, transport.Client, transport.Credential, transport.DeviceMode
	remaining := time.Until(healthDeadline)
	if remaining <= 0 {
		printErr("relay health check exceeded its %.3gs total time budget before the Relay request", healthOpts.MaxTimeSeconds)
		return 1
	}
	connectTimeout := time.Duration(healthOpts.ConnectTimeoutSeconds * float64(time.Second))
	if connectTimeout > remaining {
		connectTimeout = remaining
	}
	client := newRelayHTTPClient(connectTimeout.Seconds(), remaining.Seconds())
	// Legacy mode uses the health command's explicit connect/max timeouts; device
	// mode keeps the SPKI-pinned TLS transport but applies those same timeouts to
	// it, so a hung paired relay does not block on the fixed pairing timeout.
	if transport.Via == relayViaLocal && !deviceMode {
		transportClient = client
	} else {
		applyHealthTimeouts(transportClient, connectTimeout.Seconds(), remaining.Seconds())
	}

	url := strings.TrimRight(baseURL, "/") + "/health"
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, url, nil)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := transportClient.Do(req)
	if err != nil {
		printErr("%s", relayRequestOutcomeUnknownMessage(baseURL, err))
		return 1
	}
	defer resp.Body.Close()

	if isHTTPRedirect(resp.StatusCode) {
		printRelayHTTPOutcomeUnknown(resp.StatusCode)
		return 1
	}
	bodyBytes, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err != nil {
		printRelayPostRequestError("reading the Relay health response", err)
		return 1
	}
	bodyBytes, err = normalizeUTF8Bytes(bodyBytes, "Relay health response")
	if err != nil {
		printRelayPostRequestError("validating the Relay health response", err)
		return 1
	}
	if len(bodyBytes) == 0 {
		printRelayPostRequestError(
			"validating the Relay health response",
			errors.New("Relay health response was empty"),
		)
		return 1
	}

	// Cache-only nudge instead of the previous inline checkForUpdate: that one
	// hit GitHub on a stale cache with the default 15s client, silently
	// unbounding a probe whose whole contract is caller-bounded timeouts.
	maybeNudgeSkillUpdate(paths, false)

	_, _ = os.Stdout.Write(bodyBytes)
	if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}

	if notice := checkRelayVersion(paths, bodyBytes); !notice.empty() {
		printHumanNotice(notice)
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		printRelayHTTPOutcomeUnknown(resp.StatusCode)
		return 1
	}
	if resp.StatusCode >= 400 {
		return 1
	}
	return 0
}

// readAllLimited mirrors the relay-side 256 MiB response ceiling so a
// misbehaving upstream cannot make the CLI buffer unbounded output.
func readAllLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("relay response exceeded the %d-byte limit — narrow the request (filter, pagination, or a more specific path)", maxBytes)
	}
	return data, nil
}

func relayConnectErrorMessage(baseURL string, err error) string {
	return fmt.Sprintf(
		"cannot reach the NOVA Relay at %s: %s\nCheck that the NOVA Relay App is running in Home Assistant, then run: ha-nova doctor",
		strings.TrimRight(baseURL, "/"), err,
	)
}

func relayRequestOutcomeUnknownMessage(baseURL string, err error) string {
	if strings.TrimRight(baseURL, "/") == cloudRelayVirtualBaseURL {
		return fmt.Sprintf(
			"OUTCOME_UNKNOWN: Home Assistant Cloud could not complete the NOVA Relay request: %s\nThe request outcome is unknown: it may have reached the Relay. Verify the target state; do not retry automatically.",
			cloudOutcomeUnknownCause(err),
		)
	}
	return fmt.Sprintf(
		"OUTCOME_UNKNOWN: %s\nThe request outcome is unknown: it may have reached the Relay. Verify the target state; do not retry automatically.",
		relayConnectErrorMessage(baseURL, err),
	)
}

type relayOutcomeUnknownError struct {
	message string
	cause   error
}

func (e *relayOutcomeUnknownError) Error() string {
	return e.message
}

func (e *relayOutcomeUnknownError) Unwrap() error {
	return e.cause
}

func relayRequestOutcomeUnknownError(baseURL string, err error) error {
	return &relayOutcomeUnknownError{
		message: relayRequestOutcomeUnknownMessage(baseURL, err),
		cause:   err,
	}
}

func cloudOutcomeUnknownCause(err error) string {
	var cloudErr *CloudError
	if errors.As(err, &cloudErr) {
		return cloudErr.Error()
	}
	return "Cloud transport failed"
}

func relayPostRequestOutcomeUnknownError(stage string, err error) error {
	return &relayOutcomeUnknownError{
		message: fmt.Sprintf(
			"OUTCOME_UNKNOWN: Relay request was already sent and may have succeeded, but %s failed: %s\nVerify the target state; do not retry automatically.",
			stage,
			err,
		),
		cause: err,
	}
}

func printRelayPostRequestError(stage string, err error) {
	printErr("%s", relayPostRequestOutcomeUnknownError(stage, err))
}

func relayHTTPOutcomeUnknownError(status int) error {
	return &relayOutcomeUnknownError{
		message: fmt.Sprintf(
			"OUTCOME_UNKNOWN: Relay returned HTTP %d after the request was sent. The request outcome is unknown and may have succeeded; verify the target state and do not retry automatically.",
			status,
		),
	}
}

func printRelayHTTPOutcomeUnknown(status int) {
	printErr("%s", relayHTTPOutcomeUnknownError(status))
}

// shouldWarnRelayOutdated throttles the ws/core outdated-relay warning to one
// per hour across CLI invocations; skill flows issue many relay calls in a
// row and must not see the warning on every one. `relay health` and doctor
// stay unthrottled as the explicit diagnostic paths.
func shouldWarnRelayOutdated(paths runtimePaths) bool {
	allowed := true
	mutateActiveInstallCache(paths, func() {
		marker := filepath.Join(paths.CacheDir, "relay-outdated-warned")
		if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < time.Hour {
			allowed = false
			return
		}
		if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
			return
		}
		_ = os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	})
	return allowed
}
