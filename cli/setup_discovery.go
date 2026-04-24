package main

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var resolveHAURLBaseWithinTimeoutForDiscovery = resolveHomeAssistantURLBaseWithinTimeout
var discoverHAViaMDNSForDiscovery = discoverHAViaMDNS
var collectARPHostsForDiscovery = collectARPHosts
var runMDNSBrowseForDiscovery = runMDNSBrowse
var runMDNSLookupForDiscovery = runMDNSLookup
var mdnsAvailableForDiscovery = defaultMDNSDiscoveryAvailable
var setupDiscoveryOverallTimeout = 20 * time.Second
var setupDiscoveryPlatformOS = runtime.GOOS

func detectDefaultHAHost(cfg runtimeConfig) string {
	host, _ := detectDefaultHAHostChoice(cfg)
	return host
}

func detectDefaultHAHostChoice(cfg runtimeConfig) (string, bool) {
	deadline := time.Now().Add(setupDiscoveryOverallTimeout)
	for _, candidate := range collectCandidateHosts(cfg) {
		if candidate == "" {
			continue
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if _, err := resolveHAURLBaseWithinTimeoutForDiscovery(candidate, remaining); err == nil {
			return candidate, true
		}
	}
	return preferredUnverifiedHAHost(cfg), false
}

func preferredUnverifiedHAHost(cfg runtimeConfig) string {
	for _, candidate := range []string{cfg.HAHost, cfg.HAURL, cfg.RelayBaseURL} {
		if host := normalizeHostInput(candidate); host != "" {
			return host
		}
	}
	return ""
}

func collectCandidateHosts(cfg runtimeConfig) []string {
	candidates := []string{}
	appendUnique := func(value string) {
		host := normalizeHostInput(value)
		if host == "" {
			return
		}
		for _, existing := range candidates {
			if existing == host {
				return
			}
		}
		candidates = append(candidates, host)
	}

	appendUnique(cfg.HAHost)
	appendUnique(cfg.HAURL)
	appendUnique(cfg.RelayBaseURL)
	appendUnique(discoverHAViaMDNSForDiscovery())
	appendUnique("homeassistant.local")
	appendUnique("home-assistant.local")
	appendUnique("hass.local")
	for _, candidate := range collectARPHostsForDiscovery() {
		appendUnique(candidate)
	}

	return candidates
}

func discoverHAViaMDNS() string {
	if !mdnsAvailableForDiscovery() {
		return ""
	}

	instanceOut, err := runMDNSBrowseForDiscovery()
	if err != nil {
		return ""
	}
	if setupDiscoveryPlatformOS == "linux" {
		return parseAvahiBrowseHost(instanceOut)
	}
	instance := parseMDNSBrowseInstance(instanceOut)
	if instance == "" {
		return ""
	}

	txtOut, err := runMDNSLookupForDiscovery(instance)
	if err != nil {
		return ""
	}
	return parseMDNSLookupHost(txtOut)
}

func collectARPHosts() []string {
	if _, err := exec.LookPath("arp"); err != nil {
		return nil
	}
	args := []string{"-an"}
	if runtime.GOOS == "windows" {
		args = []string{"-a"}
	}
	out, err := exec.Command("arp", args...).Output()
	if err != nil {
		return nil
	}
	return parseARPHosts(string(out))
}

func parseARPHosts(output string) []string {
	re := regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	lines := strings.Split(output, "\n")
	matches := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "interface:") || strings.Contains(lower, "internet address") {
			continue
		}
		lineMatches := re.FindAllString(line, -1)
		if len(lineMatches) == 0 {
			continue
		}
		matches = append(matches, lineMatches...)
	}
	if len(matches) == 0 {
		return nil
	}

	capHint := len(matches)
	if capHint > 4 {
		capHint = 4
	}
	hosts := make([]string, 0, capHint)
	seen := map[string]struct{}{}
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		hosts = append(hosts, match)
		if len(hosts) == 4 {
			break
		}
	}
	return hosts
}

func defaultMDNSDiscoveryAvailable() bool {
	var binary string
	switch setupDiscoveryPlatformOS {
	case "darwin":
		binary = "dns-sd"
	case "linux":
		binary = "avahi-browse"
	default:
		return false
	}
	_, err := exec.LookPath(binary)
	return err == nil
}

func runMDNSBrowse() (string, error) {
	switch setupDiscoveryPlatformOS {
	case "darwin":
		return runCommandAllowingTimeoutOutput(3*time.Second, "dns-sd", "-B", "_home-assistant._tcp", "local")
	case "linux":
		return runCommandAllowingTimeoutOutput(3*time.Second, "avahi-browse", "-rt", "_home-assistant._tcp")
	default:
		return "", exec.ErrNotFound
	}
}

func runMDNSLookup(instance string) (string, error) {
	return runCommandAllowingTimeoutOutput(3*time.Second, "dns-sd", "-L", instance, "_home-assistant._tcp", "local")
}

func runCommandAllowingTimeoutOutput(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	if ctx.Err() == context.DeadlineExceeded && stdout.Len() > 0 {
		return stdout.String(), nil
	}
	return "", err
}

func parseMDNSBrowseInstance(output string) string {
	re := regexp.MustCompile(`(?m)^\s*\S+\s+Add\b.*\s_home-assistant\._tcp\.\s+(.+?)\s*$`)
	match := re.FindStringSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func parseMDNSLookupHost(output string) string {
	for _, marker := range []string{"internal_url=", "base_url="} {
		if idx := strings.Index(output, marker); idx >= 0 {
			value := output[idx+len(marker):]
			value = strings.Fields(value)[0]
			return normalizeHostInput(value)
		}
	}
	return ""
}

func parseAvahiBrowseHost(output string) string {
	urlPattern := regexp.MustCompile(`(?:internal_url|base_url)=([^"\s]+)`)
	if match := urlPattern.FindStringSubmatch(output); len(match) >= 2 {
		return normalizeHostInput(match[1])
	}

	addressPattern := regexp.MustCompile(`(?m)^\s*address = \[([^\]]+)\]\s*$`)
	matches := addressPattern.FindAllStringSubmatch(output, -1)
	for _, match := range matches {
		candidate := strings.TrimSpace(match[1])
		if ip := net.ParseIP(candidate); ip != nil && ip.To4() != nil {
			return candidate
		}
	}
	if len(matches) > 0 {
		return strings.TrimSpace(matches[0][1])
	}

	hostPattern := regexp.MustCompile(`(?m)^\s*hostname = \[([^\]]+)\]\s*$`)
	if match := hostPattern.FindStringSubmatch(output); len(match) >= 2 {
		return normalizeHostInput(match[1])
	}
	return ""
}
