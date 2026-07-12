package main

import (
	"bytes"
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
	}
}

type relayRequestOptions struct {
	InlineJSON    string
	JSONFile      string
	JQFilter      string
	JQFile        string
	OutputFile    string
	BinaryOutFile string
	Method        string
	Path          string
	StrictStatus  bool
}

func runRelayCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova relay <health|ws|core|files|jq|version> ...")
		return 1
	}
	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Println("Usage: ha-nova relay <health|ws|core|files|jq|version> ...")
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
	case "jq":
		return runJQ(args[1:])
	case "version":
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
	case "ws", "files":
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

	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova relay "+command+" [flags]") {
			return opts, errHelpShown
		}
		return opts, err
	}
	if command == "core" {
		if opts.Method == "" || opts.Path == "" {
			return opts, errors.New("--method and --path are required for relay core")
		}
	}
	if opts.BinaryOutFile != "" {
		if opts.JQFilter != "" || opts.JQFile != "" {
			return opts, errors.New("--out-binary cannot be combined with --jq/--jq-file: the binary body is decoded from the raw envelope")
		}
		if opts.OutputFile != "" {
			return opts, errors.New("use either --out or --out-binary, not both")
		}
	}
	return opts, nil
}

func loadRelayPayload(opts relayRequestOptions) ([]byte, error) {
	switch {
	case opts.InlineJSON != "" && opts.JSONFile != "":
		return nil, errors.New("use either inline JSON or a file, not both")
	case opts.JSONFile != "":
		return os.ReadFile(opts.JSONFile)
	case opts.InlineJSON != "":
		payload := normalizeInlineJSON(opts.InlineJSON)
		if !json.Valid([]byte(payload)) {
			return nil, errors.New("inline JSON payload is not valid JSON; on PowerShell prefer --data-file if quoting rewrites the payload")
		}
		return []byte(payload), nil
	default:
		return nil, nil
	}
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
	switch {
	case opts.JQFilter != "" && opts.JQFile != "":
		return "", errors.New("use either --jq or --jq-file, not both")
	case opts.JQFile != "":
		data, err := os.ReadFile(opts.JQFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	default:
		return opts.JQFilter, nil
	}
}

func runRelayProxy(paths runtimePaths, endpoint string, args []string) int {
	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	token, err := readRelayAuthToken()
	if err != nil {
		printErr("%s", relayAuthTokenProblemMessage(err))
		return 1
	}

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

	var requestBody []byte
	if endpoint == "core" {
		requestBody = []byte(fmt.Sprintf(`{"method":%q,"path":%q`, strings.ToUpper(opts.Method), opts.Path))
		if len(payloadBytes) > 0 {
			requestBody = append(requestBody, []byte(`,"body":`)...)
			requestBody = append(requestBody, payloadBytes...)
		}
		requestBody = append(requestBody, '}')
	} else {
		requestBody = payloadBytes
	}

	url := strings.TrimRight(cfg.RelayBaseURL, "/") + "/" + endpoint
	req, err := http.NewRequest("POST", url, bytes.NewReader(requestBody))
	if err != nil {
		printErr("%s", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		printErr("%s", relayConnectErrorMessage(cfg.RelayBaseURL, err))
		return 1
	}
	defer resp.Body.Close()

	if notice := checkRelayVersionValue(paths, resp.Header.Get(relayVersionHeader)); !notice.empty() {
		if shouldWarnRelayOutdated(paths) {
			printHumanNotice(notice)
		}
	}
	maybeNudgeSkillUpdate(paths, true)

	bodyBytes, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	upstreamExitStatus := 0
	if endpoint == "core" {
		upstreamExitStatus = relayCoreUpstreamExitStatus(bodyBytes, opts.StrictStatus)
	}

	jqFilter, err := loadJQFilter(opts)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	if jqFilter != "" {
		res, err := applyJQFilter(jqFilter, bodyBytes, false)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		bodyBytes = []byte(res.output)
	}

	if opts.BinaryOutFile != "" {
		if err := writeRelayBinaryBody(bodyBytes, opts.BinaryOutFile); err != nil {
			printErr("%s", err)
			return 1
		}
	} else if opts.OutputFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.OutputFile), 0o755); err != nil {
			printErr("%s", err)
			return 1
		}
		if err := os.WriteFile(opts.OutputFile, bodyBytes, 0o644); err != nil {
			printErr("%s", err)
			return 1
		}
	} else {
		_, _ = os.Stdout.Write(bodyBytes)
		if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
			fmt.Fprintln(os.Stdout)
		}
	}

	if resp.StatusCode >= 400 {
		return 1
	}
	if upstreamExitStatus != 0 {
		return upstreamExitStatus
	}
	return 0
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
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova relay health [--connect-timeout <s>] [--max-time <s>]") {
			return opts, errHelpShown
		}
		return opts, err
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
	client := newRelayHTTPClient(healthOpts.ConnectTimeoutSeconds, healthOpts.MaxTimeSeconds)

	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	token, err := readRelayAuthToken()
	if err != nil {
		printErr("%s", relayAuthTokenProblemMessage(err))
		return 1
	}

	url := strings.TrimRight(cfg.RelayBaseURL, "/") + "/health"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		printErr("%s", relayConnectErrorMessage(cfg.RelayBaseURL, err))
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	if err != nil || len(bodyBytes) == 0 {
		printErr("relay health check failed")
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

// shouldWarnRelayOutdated throttles the ws/core outdated-relay warning to one
// per hour across CLI invocations; skill flows issue many relay calls in a
// row and must not see the warning on every one. `relay health` and doctor
// stay unthrottled as the explicit diagnostic paths.
func shouldWarnRelayOutdated(paths runtimePaths) bool {
	marker := filepath.Join(paths.CacheDir, "relay-outdated-warned")
	if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < time.Hour {
		return false
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return true
	}
	if err := os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return true
	}
	return true
}
