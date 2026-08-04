package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

// Test seam: the offer must not fire (and then die on EOF) when stdin is not
// interactive — `ha-nova setup </dev/null` on a completed install has to keep
// exiting 0 with the done banner.
var addServerOfferStdinIsTTY = isInteractiveTTY

// Add-another-server flow (#411, multi-server layer 2). Offered only on the
// completed-setup screen — the single-server first run never sees it. It
// reuses the existing building blocks (discovery UI, relay install
// instructions, secure pairing flow, device verify); the one new mechanic is
// that every result is written to a NEW named profile by flipping the
// process-global selection seam, exactly like `ha-nova pair --server` does.
// Named profiles are device-credential-only: the legacy token path stays
// default-profile-only, so pairing runs with the legacy store marked
// unavailable and fails closed on pre-v1 relays.

func maybeOfferAddServerForCompletedSetup(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	serviceMode bool,
	lifecycleMarker ...[]byte,
) (bool, int) {
	if serviceMode || !writerSupportsTTYForSetup(out) || !addServerOfferStdinIsTTY() {
		return false, 0
	}
	// An explicit --server/env selection on `setup` is handled by the named
	// setup guard long before this screen; only the plain default run offers
	// the add flow.
	if requested, _ := requestedServerSelection(); requested != "" {
		return false, 0
	}
	fmt.Fprintln(out)
	add, err := promptWizardYesNoFromReader(
		reader,
		out,
		"Add another Home Assistant server?",
		false,
	)
	if errors.Is(err, errSetupExit) || errors.Is(err, errSetupBack) {
		return false, 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return true, 1
	}
	if !add {
		return false, 0
	}
	// The completed re-run path deliberately drops its lifecycle markers
	// before this screen — but the add flow's saves still need uninstall
	// protection, so capture a fresh generation now.
	if len(lifecycleMarker) == 0 {
		lifecycleMarker = [][]byte{
			captureInstallLifecycleGeneration(paths),
			captureCensusLifecycleMarker(paths),
		}
	}
	// attempted=true from here on — the flow's own closing output (success,
	// cancel note, or partial-profile guidance) replaces the done banner.
	return true, runAddServerFlow(reader, out, paths, lifecycleMarker...)
}

func runAddServerFlow(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	lifecycleMarker ...[]byte,
) int {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot read the existing server profiles: %s", err)
		return 1
	}
	name, nameErr := promptNewServerProfileName(reader, out, doc)
	if nameErr == errSetupBack || nameErr == errSetupExit {
		renderSetupCancelledNote(out)
		return 0
	}
	if nameErr != nil {
		printHumanErr("%s", nameErr)
		return 1
	}

	// The seam redirects both config saves (saveTargetProfileName) and the
	// device-credential slots to the new profile; restore it afterwards so
	// anything after this flow (census ask, later commands in-process) works
	// on the default profile again.
	previousProfile := activeServerProfile()
	setServerSelectionOverride(name)
	setActiveServerProfile(name)
	defer func() {
		setServerSelectionOverride("")
		setActiveServerProfile(previousProfile)
	}()

	return runAddServerStages(reader, out, paths, doc, name, lifecycleMarker...)
}

// Printed only once the profile exists on disk — before the first save there
// is nothing to finish or remove.
func printAddServerPartialGuidance(out io.Writer, name string) {
	renderSetupParagraph(out,
		fmt.Sprintf("The server %q was saved but is not paired yet.", name),
		fmt.Sprintf("Finish pairing with: ha-nova pair --server %s --relay-url <relay-url>", name),
		fmt.Sprintf("Or remove it with: ha-nova server remove %s", name),
	)
}

func runAddServerStages(
	reader *bufio.Reader,
	out io.Writer,
	paths runtimePaths,
	doc *configDocument,
	name string,
	lifecycleMarker ...[]byte,
) int {
	// A brand-new profile mints its own ProfileID directly —
	// ensureProfileIdentityForSetup would try to resolve the not-yet-saved
	// profile name and fail loud on the unknown selection. The install-wide
	// ClientInstallID must be SEEDED from the document: leaving it empty makes
	// the pairing stage mint a fresh one, which the immutability guard in
	// withProfileDocument rejects on every install that already paired
	// securely (the cmd_pair precedent seeds it the same way).
	cfg := runtimeConfig{ClientInstallID: doc.meta.ClientInstallID}
	if err := ensureProfileID(&cfg); err != nil {
		printHumanErr("cannot prepare the server profile identity: %s", err)
		return 1
	}

	host, haURL, hostErr := selectAddServerHAHost(reader, out, doc)
	if hostErr == errSetupBack || hostErr == errSetupExit {
		renderSetupCancelledNote(out)
		return 0
	}
	if hostErr != nil {
		printHumanErr("%s", hostErr)
		return 1
	}
	cfg = applySelectedSetupHost(cfg, host, haURL, "")
	if err := saveSetupConfigWithLifecycle(paths, cfg, lifecycleMarker...); err != nil {
		printHumanErr("cannot save the new server profile: %s", err)
		return 1
	}
	// From here on the profile exists on disk: a cancel must still say how to
	// finish or remove it.
	cancelKeepingPartialProfile := func() int {
		renderSetupCancelledNote(out)
		printAddServerPartialGuidance(out, name)
		return 0
	}
	failKeepingPartialProfile := func() int {
		printAddServerPartialGuidance(out, name)
		return 1
	}

	repositoryURL := haAddRepositoryURL(cfg.HAURL)
	renderSetupParagraph(out,
		"Next, install the NOVA Relay App on this Home Assistant instance.",
	)
	renderSetupLink(out, "Add the repository:", repositoryURL)
	renderSetupIndentedBlock(out, "Then:", "    ",
		"1. Go to Settings > Apps > App Store (on older Home Assistant: Settings > Add-ons)",
		`2. Search for "NOVA Relay"`,
		"3. Click Install and wait for it to finish",
		"4. Click Start",
	)
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter when the app is running", ""); err != nil {
		if err == errSetupBack || err == errSetupExit {
			return cancelKeepingPartialProfile()
		}
		printHumanErr("%s", err)
		return failKeepingPartialProfile()
	}

	for {
		// legacyTokenStoreUnavailable=true: named profiles never use the
		// machine-wide legacy token (spec: device-credential-only, fail
		// closed on relays without secure pairing).
		_, pairErr := runSetupPairingFlow(reader, out, paths, &cfg, true, lifecycleMarker...)
		if pairErr == errSetupRelayTokenStep {
			renderSetupErrorLine(out, "Additional servers pair with the six-digit code only; the manual token path stays on the default server.")
			continue
		}
		if errors.Is(pairErr, errSetupDevicePaired) {
			break
		}
		if pairErr == errSetupBack || pairErr == errSetupExit {
			return cancelKeepingPartialProfile()
		}
		if pairErr != nil {
			printHumanErr("%s", pairErr)
			return failKeepingPartialProfile()
		}
		// A nil return would mean a legacy token exchange, which the
		// unavailable-store guard above rules out; treat it as a failure
		// rather than continuing with a token the profile must not hold.
		printHumanErr("pairing did not produce a device credential for %q", name)
		return failKeepingPartialProfile()
	}

	// After errSetupDevicePaired the credential and secure endpoint are
	// already persisted — a verification failure is a reachability problem,
	// never an unpaired profile, so the guidance must not suggest pairing
	// again (that would replace a working credential).
	renderPairedButUnverified := func() {
		renderSetupParagraph(out,
			fmt.Sprintf("The server %q is paired; only the verification could not reach it yet.", name),
			"Make sure the NOVA Relay App is running, then check with:",
			fmt.Sprintf("  ha-nova doctor --server %s", name),
		)
	}
	if !verifyDeviceHealth(cfg) {
		renderSetupErrorLine(out, "Paired, but the secure device endpoint did not answer yet. The App may still be starting.")
		if _, err := promptWizardLineFromReader(reader, out, "Press Enter to retry once", ""); err != nil {
			if err == errSetupBack || err == errSetupExit {
				renderSetupCancelledNote(out)
				renderPairedButUnverified()
				return 0
			}
			printHumanErr("%s", err)
			renderPairedButUnverified()
			return 1
		}
		if !verifyDeviceHealth(cfg) {
			printHumanErr("the secure device endpoint for %q did not answer", name)
			renderPairedButUnverified()
			return 1
		}
	}
	renderSetupSuccessLine(out, "Server %q is paired and verified", name)
	renderSetupParagraph(out,
		fmt.Sprintf("Use it per call with HA_NOVA_SERVER=%s (AI clients) or --server %s (CLI).", name, name),
		fmt.Sprintf("The default server is unchanged; switch it with: ha-nova server default %s", name),
	)
	return 0
}

func promptNewServerProfileName(
	reader *bufio.Reader,
	out io.Writer,
	doc *configDocument,
) (string, error) {
	existing := make(map[string]bool)
	for _, profile := range doc.profileNames() {
		existing[profile] = true
	}
	// "default" is always taken, even on hand-edited or pair-server-first
	// configs whose servers map lacks a literal default entry — a wizard
	// profile with that name would seize the un-suffixed credential slots and
	// the legacy mirror.
	existing[defaultServerProfileName] = true
	for {
		entered, err := promptWizardLineFromReader(
			reader,
			out,
			"Name for the new server (a-z, 0-9, '-'; e.g. cabin)",
			"",
		)
		if err != nil {
			return "", err
		}
		name := strings.TrimSpace(entered)
		if err := validateServerProfileName(name); err != nil {
			renderSetupErrorLine(out, "%s", err)
			continue
		}
		if existing[name] {
			renderSetupErrorLine(out, "A server named %q already exists. Choose a different name.", name)
			continue
		}
		return name, nil
	}
}

// selectAddServerHAHost discovers instances and hides every already-configured
// one — offering the user their existing server as "new" would only produce a
// duplicate profile against the same relay.
func selectAddServerHAHost(
	reader *bufio.Reader,
	out io.Writer,
	doc *configDocument,
) (string, string, error) {
	result := runSetupDiscoveryWithFeedback(out, runtimeConfig{})
	configured := configuredServerHostPortKeys(doc)
	expandConfiguredLocalAliases(configured)
	candidates := make([]setupDiscoveryCandidate, 0, len(result.candidates))
	for _, candidate := range result.candidates {
		if configured[addServerHostPortKey(candidate.HAURL)] ||
			configured[addServerViaHostPortKey(candidate)] {
			continue
		}
		candidates = append(candidates, candidate)
	}
	// Manually entered addresses get the same already-configured check as
	// discovery — typing the existing server would create the duplicate
	// profile the filter exists to prevent.
	promptUnconfiguredHost := func(defaultHost string) (string, string, error) {
		for {
			host, haURL, err := promptValidHAHostFromReader(reader, out, defaultHost)
			if err != nil {
				return "", "", err
			}
			if manualHostIsConfigured(configured, haURL) {
				renderSetupErrorLine(out, "This Home Assistant is already configured as a server profile. Enter a different address.")
				defaultHost = ""
				continue
			}
			return host, haURL, nil
		}
	}
	if len(candidates) == 0 {
		if len(result.candidates) > 0 {
			renderSetupParagraphTight(out, "Every discovered Home Assistant instance is already configured.")
		}
		return promptUnconfiguredHost("")
	}
	fmt.Fprintf(out, "  Found %d Home Assistant instance(s) that are not configured yet.\n", len(candidates))
	candidate, selected, err := promptSetupDiscoveryCandidateInteractive(reader, out, candidates)
	if err != nil {
		return "", "", err
	}
	if selected {
		return candidate.Host, candidate.HAURL, nil
	}
	return promptUnconfiguredHost(candidate.Host)
}

// Instance identity is host:HA-port. A port difference on the same host is a
// DIFFERENT instance (two HA containers on one Docker host), while a .local
// name and the IPv4 it resolved to are the SAME one — mDNS candidates carry
// the original name in Via, so both spellings are matched.
func addServerHostPortKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	// A bare host implies the HA default port; an explicit URL without a
	// port means the SCHEME default (80/443) — conflating https://ha.example
	// with ha.example:8123 would hide a genuinely different instance.
	hadScheme := strings.Contains(trimmed, "://")
	if !hadScheme {
		trimmed = "http://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	port := parsed.Port()
	if port == "" {
		switch {
		case !hadScheme:
			port = "8123"
		case parsed.Scheme == "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")) + ":" + port
}

func addServerViaHostPortKey(candidate setupDiscoveryCandidate) string {
	via := strings.TrimSuffix(strings.TrimSpace(candidate.Via), ".")
	if via == "" {
		return ""
	}
	// The alias must carry the SAME effective port as the candidate URL —
	// including scheme defaults — or a portless advertised URL (keyed :80 on
	// the configured side) never matches.
	urlKey := addServerHostPortKey(candidate.HAURL)
	separator := strings.LastIndex(urlKey, ":")
	if separator < 0 {
		return ""
	}
	return strings.ToLower(via) + urlKey[separator:]
}

// The manual check works in BOTH alias directions: a typed .local hostname
// also matches a profile configured by that host's resolved IPv4.
func manualHostIsConfigured(configured map[string]bool, haURL string) bool {
	key := addServerHostPortKey(haURL)
	if configured[key] {
		return true
	}
	host, port, splitErr := net.SplitHostPort(key)
	if splitErr != nil || !strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := resolveHostToIPv4ForDiscovery(host, setupDiscoveryIPResolveTimeout); ip != "" && ip != host {
		return configured[ip+":"+port]
	}
	return false
}

// Profiles configured under a .local name additionally claim their RESOLVED
// IPv4 (bounded lookup), so the same server rediscovered or manually entered
// by IP is recognized even without a Via alias.
func expandConfiguredLocalAliases(configured map[string]bool) {
	for key := range configured {
		host, port, splitErr := net.SplitHostPort(key)
		if splitErr != nil || !strings.HasSuffix(host, ".local") {
			continue
		}
		if ip := resolveHostToIPv4ForDiscovery(host, setupDiscoveryIPResolveTimeout); ip != "" && ip != host {
			configured[ip+":"+port] = true
		}
	}
}

func configuredServerHostPortKeys(doc *configDocument) map[string]bool {
	keys := make(map[string]bool)
	add := func(value string) {
		if key := addServerHostPortKey(value); key != "" {
			keys[key] = true
		}
	}
	for _, name := range doc.profileNames() {
		profile, ok := doc.flatProfile(name)
		if !ok {
			continue
		}
		add(profile.HAURL)
		// The bare HAHost implies the default port ONLY when no URL pins the
		// real one — otherwise a second instance on :8123 of the same host
		// would be hidden by a profile that actually lives on another port.
		if profile.HAURL == "" {
			add(profile.HAHost)
		}
		if profile.HAURL == "" && profile.HAHost == "" {
			add(normalizeHostInput(profile.RelayBaseURL))
		}
	}
	return keys
}
