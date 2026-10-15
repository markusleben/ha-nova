package main

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// Test seam. The unauthenticated GET /pair/v1/info answers only from a running
// NOVA Relay: probePairingV1 requires HTTP 200 plus the relay's own
// {"ok":true,"data":{"protocol_version":"v1"}} envelope, so a closed port, an
// HA or proxy page, a timeout, or an old relay all read as "not running".
var probeInstalledRelayForSetup = probePairingV1

// relayAppAlreadyRunning reports whether the NOVA Relay already answers at the
// configured relay address, so a second computer can skip the App install.
// Any doubt returns false: the caller then shows the install steps as usual.
func relayAppAlreadyRunning(out io.Writer, cfg runtimeConfig) bool {
	relayURL := strings.TrimSpace(cfg.RelayBaseURL)
	if relayURL == "" {
		relayURL = deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost)
	}
	if relayURL == "" {
		return false
	}
	renderSetupParagraphTight(out, "Checking whether NOVA Relay is already running on this Home Assistant...")
	if !probeInstalledRelayForSetup(relayURL) {
		return false
	}
	renderSetupSuccessLine(out, "NOVA Relay is already installed and running. Nothing to install on this computer.")
	return true
}

// haSupportsCombinedAppLink reports whether the Home Assistant version (from
// its mDNS announcement) knows the supervisor_app My link with
// repository_url. Home Assistant 2026.2 (frontend 20260128) added it; an
// unknown or older version keeps the two-step repository + App Store flow.
func haSupportsCombinedAppLink(version string) bool {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 3)
	if len(parts) < 2 {
		return false
	}
	year, yearErr := strconv.Atoi(parts[0])
	month, monthErr := strconv.Atoi(parts[1])
	if yearErr != nil || monthErr != nil {
		return false
	}
	return year > 2026 || (year == 2026 && month >= 2)
}

// relayInstallGuide is the browser link plus the clicks that follow it.
type relayInstallGuide struct {
	Link     string
	Browser  string
	Steps    []string
	Combined bool
}

func relayInstallGuideFor(haURL, haVersion string) relayInstallGuide {
	if haSupportsCombinedAppLink(haVersion) {
		return relayInstallGuide{
			Link:     haInstallRelayAppURL(haURL),
			Browser:  `In the browser: log in to Home Assistant if asked, then click "Add" when it asks to add the app repository.`,
			Steps:    []string{"1. Click Install and wait for it to finish", "2. Click Start"},
			Combined: true,
		}
	}
	return relayInstallGuide{
		Link:    haAddRepositoryURL(haURL),
		Browser: `In the browser: log in to Home Assistant if asked, then click "Add" to confirm the repository.`,
		Steps: []string{
			"1. Go to Settings > Apps > App Store (on older Home Assistant: Settings > Add-ons)",
			`2. Search for "NOVA Relay"`,
			"3. Click Install and wait for it to finish",
			"4. Click Start",
		},
	}
}

func (guide relayInstallGuide) stepsTitle() string {
	if guide.Combined {
		return "On the NOVA Relay page:"
	}
	return "Once the repository is added:"
}

// runRelayInstallSteps announces the install link, opens it on Enter, lists
// the clicks, and waits until the user says the App runs. It returns the
// prompt's errSetupBack/errSetupExit unchanged for the caller's navigation.
func runRelayInstallSteps(reader *bufio.Reader, out io.Writer, guide relayInstallGuide) error {
	renderSetupParagraph(out, "Next, install the NOVA Relay App in your Home Assistant.")
	renderSetupLink(out, "This will open:", guide.Link)
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", ""); err != nil {
		return err
	}
	openAnnouncedBrowserURL(out, guide.Link)
	renderSetupParagraphTight(out, guide.Browser)
	renderSetupIndentedBlock(out, guide.stepsTitle(), "    ", guide.Steps...)
	_, err := promptWizardLineFromReader(reader, out, "Press Enter when the app is running", "")
	return err
}
