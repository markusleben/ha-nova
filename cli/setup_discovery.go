package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brutella/dnssd"
)

var resolveHAURLBaseWithinTimeoutForDiscovery = resolveHomeAssistantURLBaseWithinTimeout
var discoverHAViaMDNSForDiscovery = discoverHAViaMDNS
var lookupDNSSDForDiscovery = dnssd.LookupType
var setupDiscoveryOverallTimeout = 6 * time.Second
var resolveHostToIPv4ForDiscovery = resolveHostToIPv4
var setupDiscoveryIPResolveTimeout = 2 * time.Second
var setupDiscoveryIPProbeTimeout = 3 * time.Second
var setupDiscoveryMDNSTimeout = 2 * time.Second
var setupDiscoveryCandidateProbeReserve = 2 * time.Second

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
	// Via carries the advertised .local hostname when the discovery path
	// rewrote it to an IPv4 URL — the add-server filter matches it against
	// profiles configured with the .local spelling.
	Via string
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
	fallback := preferredUnverifiedHAHost(cfg)
	if input, source := preferredConfiguredHAInput(cfg); input != "" {
		timeout := min(
			setupDiscoveryMaxProbeTimeout,
			time.Until(deadline)-
				setupDiscoveryMDNSTimeout-
				setupDiscoveryCandidateProbeReserve,
		)
		if timeout > 0 {
			resolved, err :=
				resolveHAURLBaseWithinTimeoutForDiscovery(input, timeout)
			if err == nil {
				return []setupDiscoveryCandidate{{
					Host:   normalizeHostInput(resolved),
					HAURL:  strings.TrimRight(resolved, "/"),
					Source: source,
				}}, fallback
			}
		}
	}

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
			Via:    probe.Via,
		}
		if probeDeadline.After(deadline) {
			probeDeadline = deadline
		}
		if host, haURL := confirmedDiscoveryIPv4(candidate.HAURL, probeDeadline); host != "" {
			candidate.Host = host
			candidate.HAURL = haURL
			if candidate.Via == "" {
				candidate.Via = normalizeHostInput(probe.Host)
			}
		}
		key := setupDiscoveryEndpointKey(candidate.HAURL)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		found = append(found, candidate)
	}

	return found, fallback
}

func confirmedDiscoveryIPv4(haURL string, deadline time.Time) (string, string) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return "", ""
	}
	parsed, err := url.Parse(haURL)
	if err != nil || parsed.Scheme != "http" {
		return "", ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !strings.HasSuffix(host, ".local") {
		return "", ""
	}
	ip := resolveHostToIPv4ForDiscovery(host, min(setupDiscoveryIPResolveTimeout, remaining))
	if ip == "" || ip == host {
		return "", ""
	}
	port := parsed.Port()
	parsed.Host = ip
	if port != "" {
		parsed.Host = net.JoinHostPort(ip, port)
	}
	remaining = time.Until(deadline)
	if remaining <= 0 {
		return "", ""
	}
	resolved, err := resolveHAURLBaseWithinTimeoutForDiscovery(
		parsed.String(),
		min(setupDiscoveryIPProbeTimeout, remaining),
	)
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
	appendUnique := func(probe setupDiscoveryProbe) {
		probe.Host = strings.TrimSpace(probe.Host)
		key := setupDiscoveryEndpointKey(probe.Host)
		if key == "" || len(candidates) >= setupDiscoveryMaxCandidateCount {
			return
		}
		for _, existing := range candidates {
			if setupDiscoveryEndpointKey(existing.Host) == key {
				return
			}
		}
		candidates = append(candidates, probe)
	}

	appendUnique(setupDiscoveryProbe{Host: cfg.HAHost, Source: "saved Home Assistant address"})
	appendUnique(setupDiscoveryProbe{Host: cfg.HAURL, Source: "saved Home Assistant address"})
	appendUnique(setupDiscoveryProbe{Host: normalizeHostInput(cfg.RelayBaseURL), Source: "saved Relay address"})
	for _, discovered := range discoverHAViaMDNSForDiscovery() {
		// Pass the whole probe through — flattening to (Host, Source) would
		// drop the advertised-.local Via the add-server filter relies on.
		appendUnique(discovered)
	}

	return candidates
}

func setupDiscoveryEndpointKey(value string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Host != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		return parsed.String()
	}
	return strings.ToLower(trimmed)
}

func preferredConfiguredHAInput(cfg runtimeConfig) (string, string) {
	if value := strings.TrimSpace(cfg.HAURL); value != "" {
		return value, "saved Home Assistant address"
	}
	if value := strings.TrimSpace(cfg.HAHost); value != "" {
		return value, "saved Home Assistant address"
	}
	if value := normalizeHostInput(cfg.RelayBaseURL); value != "" {
		return value, "saved Relay address"
	}
	return "", ""
}

func preferredUnverifiedHAHost(cfg runtimeConfig) string {
	for _, candidate := range []string{cfg.HAHost, cfg.HAURL, cfg.RelayBaseURL} {
		if host := normalizeHostInput(candidate); host != "" {
			return host
		}
	}
	return ""
}

func discoverHAViaMDNS() []setupDiscoveryProbe {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		setupDiscoveryMDNSTimeout,
	)
	defer cancel()
	discovered := []setupDiscoveryProbe{}
	seen := map[string]int{}
	err := lookupDNSSDForDiscovery(
		ctx,
		"_home-assistant._tcp.local.",
		func(entry dnssd.BrowseEntry) {
			endpoint := homeAssistantDNSSDURL(entry)
			if endpoint == "" {
				return
			}
			key := setupDiscoveryEndpointKey(endpoint)
			if idx, exists := seen[key]; exists {
				// A duplicate announcement may be the one carrying the
				// .local alias — losing it would let the add-server filter
				// miss a profile configured with that spelling.
				if discovered[idx].Via == "" {
					discovered[idx].Via = dnssdAdvertisedLocalHost(entry)
				}
				return
			}
			seen[key] = len(discovered)
			source := "mDNS"
			if name := safeDNSSDName(entry.Text["location_name"]); name != "" {
				source += ": " + name
			} else if name := safeDNSSDName(entry.Name); name != "" {
				source += ": " + name
			}
			discovered = append(discovered, setupDiscoveryProbe{
				Host:   endpoint,
				Source: source,
				Via:    dnssdAdvertisedLocalHost(entry),
			})
		},
		func(dnssd.BrowseEntry) {},
	)
	if err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	sort.Slice(discovered, func(i, j int) bool {
		left := setupDiscoveryEndpointKey(discovered[i].Host)
		right := setupDiscoveryEndpointKey(discovered[j].Host)
		if left == right {
			return discovered[i].Source < discovered[j].Source
		}
		return left < right
	})
	return discovered
}

// The advertised .local hostname survives the IPv4 rewrite in
// homeAssistantDNSSDURL only through this side channel. Without an
// internal_url the record's own .local entry.Host is the advertised name
// that got rewritten.
func dnssdAdvertisedLocalHost(entry dnssd.BrowseEntry) string {
	internalURL := strings.TrimSpace(entry.Text["internal_url"])
	if internalURL == "" {
		host := strings.TrimSuffix(strings.TrimSpace(entry.Host), ".")
		if strings.HasSuffix(strings.ToLower(host), ".local") {
			return normalizeHostInput(host)
		}
		return ""
	}
	parsed, err := url.Parse(internalURL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !strings.HasSuffix(host, ".local") {
		return ""
	}
	return normalizeHostInput(host)
}

func safeDNSSDName(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, strings.TrimSpace(value))
}

func homeAssistantDNSSDURL(entry dnssd.BrowseEntry) string {
	uuid := strings.TrimSpace(entry.Text["uuid"])
	if len(uuid) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(uuid); err != nil {
		return ""
	}
	internalURL := strings.TrimSpace(entry.Text["internal_url"])
	if internalURL == "" {
		host := strings.TrimSuffix(strings.TrimSpace(entry.Host), ".")
		if !strings.EqualFold(host, uuid+".local") || entry.Port < 1 || entry.Port > 65535 {
			return ""
		}
		for _, ip := range entry.IPs {
			if v4 := ip.To4(); v4 != nil {
				return "http://" + net.JoinHostPort(v4.String(), strconv.Itoa(entry.Port))
			}
		}
		for _, ip := range entry.IPs {
			if ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() &&
				!ip.IsLinkLocalUnicast() {
				return "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(entry.Port))
			}
		}
		return "http://" + net.JoinHostPort(host, strconv.Itoa(entry.Port))
	}
	parsed, err := url.Parse(internalURL)
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Hostname() == "" ||
		parsed.User != nil {
		return ""
	}
	if parsed.Scheme == "http" && strings.HasSuffix(
		strings.TrimSuffix(strings.ToLower(parsed.Hostname()), "."),
		".local",
	) {
		for _, ip := range entry.IPs {
			if v4 := ip.To4(); v4 != nil {
				port := parsed.Port()
				parsed.Host = v4.String()
				if port != "" {
					parsed.Host = net.JoinHostPort(v4.String(), port)
				}
				break
			}
		}
	}
	return strings.TrimRight(parsed.String(), "/")
}
