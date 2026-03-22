package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

var resolveHAURLBaseForFlags = resolveHomeAssistantURLBase

func normalizeHostInput(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.LastIndex(trimmed, "@"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	if strings.HasPrefix(trimmed, "[") {
		if idx := strings.Index(trimmed, "]"); idx >= 0 {
			return trimmed[1:idx]
		}
	}
	if host, _, found := strings.Cut(trimmed, ":"); found {
		return host
	}
	return trimmed
}

func guessHomeAssistantURLBase(input string) string {
	normalized := strings.TrimSpace(strings.TrimRight(input, "/"))
	if strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://") {
		return normalized
	}
	host := normalizeHostInput(normalized)
	if strings.Contains(normalized, ":") {
		return "http://" + normalized
	}
	return "http://" + host + ":8123"
}

func resolveHomeAssistantURLBase(input string) (string, error) {
	return resolveHomeAssistantURLBaseWithinTimeout(input, 0)
}

func resolveHomeAssistantURLBaseWithinTimeout(input string, timeout time.Duration) (string, error) {
	normalized := strings.TrimSpace(strings.TrimRight(input, "/"))
	host := normalizeHostInput(normalized)

	candidates := []string{}
	switch {
	case strings.HasPrefix(normalized, "http://") || strings.HasPrefix(normalized, "https://"):
		candidates = append(candidates, normalized)
	case strings.Contains(normalized, ":"):
		candidates = append(candidates, "http://"+normalized, "https://"+normalized)
	default:
		candidates = append(candidates, "http://"+host+":8123", "http://"+host, "https://"+host)
	}

	client := httpClient
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for _, candidate := range candidates {
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return "", fmt.Errorf("could not reach Home Assistant at %s", input)
			}
			timeoutClient := *httpClient
			timeoutClient.Timeout = remaining
			client = &timeoutClient
		}
		if err := probeHTTPWithClient(client, candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not reach Home Assistant at %s", input)
}

func promptValidHAHost(in io.Reader, out io.Writer, defaultHost string) (string, string, error) {
	return promptValidHAHostFromReader(bufio.NewReader(in), out, defaultHost)
}

func promptValidHAHostFromReader(reader *bufio.Reader, out io.Writer, defaultHost string) (string, string, error) {
	currentDefault := defaultHost
	for {
		input, err := promptWizardLineFromReader(reader, out, "Home Assistant address (IP, hostname, or URL)", currentDefault)
		if err != nil {
			return "", "", err
		}
		host := normalizeHostInput(input)
		if host == "" {
			renderSetupErrorLine(out, "No address entered.")
			retry, err := promptWizardYesNoFromReader(reader, out, "Try again?", true)
			if err != nil {
				return "", "", err
			}
			if !retry {
				return "", "", fmt.Errorf("setup needs a Home Assistant address to continue")
			}
			continue
		}

		resolved, err := resolveHAURLBaseWithFeedback(out, input)
		if err == nil {
			renderSetupSuccessLine(out, "Connected to Home Assistant at %s", host)
			return host, resolved, nil
		}

		renderSetupErrorLine(out, "Could not reach Home Assistant at: %s", input)
		fmt.Fprintln(out, "  Make sure Home Assistant is running and reachable from this computer.")
		if resolveSetupUISession(out).animatesSpinner() {
			armSetupNextPromptSkipsStaleBlankInput()
		}
		retry, retryErr := promptWizardYesNoFromReader(reader, out, "Try a different address?", true)
		if retryErr != nil {
			return "", "", retryErr
		}
		if retry {
			currentDefault = input
			continue
		}
		cont, contErr := promptWizardYesNoFromReader(reader, out, "Continue anyway (connection will be verified later)?", false)
		if contErr != nil {
			return "", "", contErr
		}
		if !cont {
			return "", "", fmt.Errorf("setup needs a reachable Home Assistant to continue")
		}
		return host, guessHomeAssistantURLBase(input), nil
	}
}

func deriveRelayURLFromHA(haURL, host string) string {
	if parsed, err := url.Parse(haURL); err == nil && parsed.Host != "" {
		hostname := parsed.Hostname()
		if hostname != "" {
			return "http://" + hostname + ":8791"
		}
	}
	if host == "" {
		return ""
	}
	return "http://" + host + ":8791"
}

func applySelectedSetupHost(cfg runtimeConfig, host, haURL, relayURLOverride string) runtimeConfig {
	if strings.TrimSpace(host) != "" {
		cfg.HAHost = normalizeHostInput(host)
	}
	if strings.TrimSpace(haURL) != "" {
		cfg.HAURL = strings.TrimSpace(haURL)
	}
	if strings.TrimSpace(relayURLOverride) != "" {
		cfg.RelayBaseURL = strings.TrimSpace(relayURLOverride)
		return cfg
	}
	if cfg.HAHost == "" && cfg.HAURL != "" {
		cfg.HAHost = normalizeHostInput(cfg.HAURL)
	}
	if cfg.HAHost != "" || cfg.HAURL != "" {
		cfg.RelayBaseURL = deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
	}
	return cfg
}

func applySetupFlagOverrides(cfg runtimeConfig, hostFlag, haURLFlag, relayURLFlag string) (runtimeConfig, error) {
	switch {
	case strings.TrimSpace(hostFlag) != "":
		resolvedHAURL := strings.TrimSpace(haURLFlag)
		if resolvedHAURL == "" {
			resolved, err := resolveHAURLBaseForFlags(hostFlag)
			if err != nil {
				return cfg, err
			}
			resolvedHAURL = resolved
		}
		return applySelectedSetupHost(cfg, hostFlag, resolvedHAURL, relayURLFlag), nil
	case strings.TrimSpace(haURLFlag) != "":
		return applySelectedSetupHost(cfg, haURLFlag, haURLFlag, relayURLFlag), nil
	case strings.TrimSpace(relayURLFlag) != "":
		cfg.RelayBaseURL = strings.TrimSpace(relayURLFlag)
		return cfg, nil
	default:
		return cfg, nil
	}
}
