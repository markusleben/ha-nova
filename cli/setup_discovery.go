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
var setupDiscoveryPlatformOS = runtime.GOOS
var setupDiscoveryOverallTimeout = 20 * time.Second
var resolveHostToIPv4ForDiscovery = resolveHostToIPv4
var setupDiscoveryIPResolveTimeout = 2 * time.Second
var setupDiscoveryIPProbeTimeout = 3 * time.Second

const setupDiscoveryMaxCandidateCount = 12

var setupDiscoveryMaxProbeTimeout = 3 * time.Second

type setupDiscoveryCandidate struct {
	Host   string
	HAURL  string
	Via    string
	Source string
}

type setupDiscoveryProbe struct {
	Host   string
	Source string
}

func detectDefaultHAHost(cfg runtimeConfig) string {
	host, _, _ := detectDefaultHAHostChoice(cfg)
	return host
}

// detectDefaultHAHostChoice preserves the single-result helper used by focused
// discovery tests while sharing the production all-candidate implementation.
func detectDefaultHAHostChoice(cfg runtimeConfig) (string, string, bool) {
	found, fallback := discoverReachableHAHosts(cfg)
	if len(found) == 0 {
		return fallback, "", false
	}
	return found[0].Host, found[0].Via, true
}

// discoverReachableHAHosts probes every bounded candidate instead of silently
// stopping at the first reachable Home Assistant. Results retain candidate
// priority and are deduplicated after .local names are resolved to a confirmed
// IP address.
func discoverReachableHAHosts(cfg runtimeConfig) ([]setupDiscoveryCandidate, string) {
	deadline := time.Now().Add(setupDiscoveryOverallTimeout)
	probes := collectDiscoveryProbes(cfg)
	found := make([]setupDiscoveryCandidate, 0, len(probes))
	seen := map[string]struct{}{}

	for idx, probe := range probes {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		probesLeft := len(probes) - idx
		probeTimeout := remaining / time.Duration(probesLeft)
		if probeTimeout > setupDiscoveryMaxProbeTimeout {
			probeTimeout = setupDiscoveryMaxProbeTimeout
		}
		if probeTimeout <= 0 {
			break
		}
		probeDeadline := time.Now().Add(probeTimeout)

		resolved, err := resolveHAURLBaseWithinTimeoutForDiscovery(probe.Host, probeTimeout)
		if err != nil {
			continue
		}
		candidate := setupDiscoveryCandidate{
			Host:   normalizeHostInput(resolved),
			HAURL:  strings.TrimRight(resolved, "/"),
			Source: probe.Source,
		}
		if probeDeadline.After(deadline) {
			probeDeadline = deadline
		}
		if host, haURL := confirmedDiscoveryIPv4(probe.Host, probeDeadline); host != "" {
			candidate.Host = host
			candidate.HAURL = haURL
			candidate.Via = probe.Host
		}
		key := strings.ToLower(normalizeHostInput(candidate.Host))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		found = append(found, candidate)
	}

	return found, preferredUnverifiedHAHost(cfg)
}

func confirmedDiscoveryIPv4(host string, deadline time.Time) (string, string) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return "", ""
	}
	trimmed := strings.TrimSuffix(strings.ToLower(host), ".")
	if !strings.HasSuffix(trimmed, ".local") {
		return "", ""
	}
	ip := resolveHostToIPv4ForDiscovery(host, min(setupDiscoveryIPResolveTimeout, remaining))
	if ip == "" || ip == host {
		return "", ""
	}
	remaining = time.Until(deadline)
	if remaining <= 0 {
		return "", ""
	}
	resolved, err := resolveHAURLBaseWithinTimeoutForDiscovery(ip, min(setupDiscoveryIPProbeTimeout, remaining))
	if err != nil {
		return "", ""
	}
	return ip, strings.TrimRight(resolved, "/")
}

func resolveHostToIPv4(host string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if v4 := addr.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func collectDiscoveryProbes(cfg runtimeConfig) []setupDiscoveryProbe {
	candidates := []setupDiscoveryProbe{}
	appendUnique := func(value, source string) {
		host := normalizeHostInput(value)
		if host == "" || len(candidates) >= setupDiscoveryMaxCandidateCount {
			return
		}
		for _, existing := range candidates {
			if strings.EqualFold(existing.Host, host) {
				return
			}
		}
		candidates = append(candidates, setupDiscoveryProbe{Host: host, Source: source})
	}

	appendUnique(cfg.HAHost, "saved Home Assistant address")
	appendUnique(cfg.HAURL, "saved Home Assistant address")
	appendUnique(cfg.RelayBaseURL, "saved Relay address")
	appendUnique(discoverHAViaMDNSForDiscovery(), "mDNS")
	appendUnique("homeassistant.local", "common network name")
	appendUnique("home-assistant.local", "common network name")
	appendUnique("hass.local", "common network name")
	for _, candidate := range collectARPHostsForDiscovery() {
		appendUnique(candidate, "local network cache")
	}

	return candidates
}

func preferredUnverifiedHAHost(cfg runtimeConfig) string {
	for _, candidate := range []string{cfg.HAHost, cfg.HAURL, cfg.RelayBaseURL} {
		if host := normalizeHostInput(candidate); host != "" {
			return host
		}
	}
	return ""
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
