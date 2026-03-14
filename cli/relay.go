package main

import (
	"bytes"
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

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	},
}

type relayRequestOptions struct {
	InlineJSON string
	JSONFile   string
	JQFilter   string
	JQFile     string
	OutputFile string
	Method     string
	Path       string
}

func runRelayCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printErr("Usage: ha-nova relay <health|ws|core|jq|version> ...")
		return 1
	}

	switch args[0] {
	case "health":
		return runHealth(paths)
	case "ws":
		return runRelayProxy(paths, "ws", args[1:])
	case "core":
		return runRelayProxy(paths, "core", args[1:])
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
	case "ws":
		fs.StringVar(&opts.InlineJSON, "data", "", "inline JSON payload")
		fs.StringVar(&opts.InlineJSON, "d", "", "inline JSON payload")
		fs.StringVar(&opts.JSONFile, "data-file", "", "path to JSON payload file")
	case "core":
		fs.StringVar(&opts.Method, "method", "", "HTTP method")
		fs.StringVar(&opts.Path, "path", "", "core API path")
		fs.StringVar(&opts.InlineJSON, "body", "", "inline JSON body")
		fs.StringVar(&opts.InlineJSON, "d", "", "inline JSON body")
		fs.StringVar(&opts.JSONFile, "body-file", "", "path to JSON body file")
	default:
		return opts, fmt.Errorf("unsupported relay command: %s", command)
	}
	fs.StringVar(&opts.JQFilter, "jq", "", "jq filter")
	fs.StringVar(&opts.JQFile, "jq-file", "", "path to jq filter file")
	fs.StringVar(&opts.OutputFile, "out", "", "write command output to file")

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if command == "core" {
		if opts.Method == "" || opts.Path == "" {
			return opts, errors.New("--method and --path are required for relay core")
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
		return []byte(opts.InlineJSON), nil
	default:
		return nil, nil
	}
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
		printErr("%s", err)
		return 1
	}

	opts, err := parseRelayFlags(endpoint, args)
	if err != nil {
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
		printErr("%s", err)
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		printErr("%s", err)
		return 1
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

	if opts.OutputFile != "" {
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
	return 0
}

func runHealth(paths runtimePaths) int {
	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	token, err := readRelayAuthToken()
	if err != nil {
		printErr("%s", err)
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

	resp, err := httpClient.Do(req)
	if err != nil {
		printErr("%s", err)
		return 1
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil || len(bodyBytes) == 0 {
		printErr("relay health check failed")
		return 1
	}

	if updateMessage := checkForUpdate(paths, true); updateMessage != "" {
		fmt.Fprintln(os.Stdout, updateMessage)
	}

	_, _ = os.Stdout.Write(bodyBytes)
	if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}

	if warning := checkRelayVersion(paths, bodyBytes); warning != "" {
		fmt.Fprintln(os.Stdout, warning)
	}

	if resp.StatusCode >= 400 {
		return 1
	}
	return 0
}
