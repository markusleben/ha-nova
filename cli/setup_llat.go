package main

import (
	"bufio"
	"io"
	"strings"
)

func runSetupLLATWalkthrough(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, relayToken string, steps setupWizardSteps) error {
	renderSetupStep(out, steps.LLAT, steps.Total, "Set up Home Assistant Access Token")
	renderSetupParagraph(out,
		"Create a Home Assistant Access Token in Home Assistant.",
		"Then paste it into NOVA Relay.",
	)
	renderSetupIndentedBlock(out,
		"In Home Assistant:",
		"    ",
		`1. Click the "Security" tab`,
		`2. Scroll down to "Long-Lived Access Tokens"`,
		`3. Click "Create Token" and name it "NOVA"`,
		"4. Copy the token that appears",
	)
	if trimmedRelayToken := strings.TrimSpace(relayToken); trimmedRelayToken != "" {
		renderSetupMutedNoteBlock(out,
			"Only if needed",
			"Still missing the Relay Auth Token in NOVA Relay?",
			"Here it is again:",
			"  "+trimmedRelayToken,
		)
	}

	renderSetupLink(out, "This will open:", haProfileSecurityURL(cfg.HAURL))
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open your HA profile", ""); err != nil {
		return err
	}
	openAnnouncedBrowserURL(out, haProfileSecurityURL(cfg.HAURL))

	renderSetupParagraph(out, "Got it? Now I'll open the NOVA Relay settings so you can paste it.")
	renderSetupLink(out, "This will open:", haRelayAppPageURL(cfg.HAURL))
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open the relay settings", ""); err != nil {
		return err
	}
	openAnnouncedBrowserURL(out, haRelayAppPageURL(cfg.HAURL))

	renderSetupIndentedBlock(out, "Finish the relay configuration:", "    ",
		`5. Open the "Configuration" tab`,
		`6. Paste the token into the "Home Assistant Access Token" field ("ha_llat")`,
		"7. Click Save",
		"8. Click Start (or Restart if already running)",
	)

	_, err := promptWizardLineFromReader(reader, out, "Press Enter when the app is running", "")
	return err
}
