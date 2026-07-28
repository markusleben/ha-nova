package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptToOpenNOVAForPairing(
	reader *bufio.Reader,
	out io.Writer,
	haURL string,
) error {
	appTarget, err := haNOVAAppPanelURL(haURL)
	if err != nil {
		return fmt.Errorf("cannot open NOVA for pairing: %w", err)
	}
	if appTarget.Direct {
		renderSetupParagraph(out,
			"Open NOVA in the Home Assistant sidebar and click \"Connect a device\" to get a six-digit code.",
			"If NOVA is not in the sidebar, the link below opens its Web UI directly.",
			"If your Home Assistant version does not support the direct link, use the NOVA sidebar item or choose \"Open Web UI\" on the NOVA App page.",
		)
	} else {
		renderSetupParagraph(out,
			"Open NOVA in the Home Assistant sidebar and click \"Connect a device\" to get a six-digit code.",
			"This development build cannot safely identify which local NOVA App panel belongs to the selected Relay. The link below opens Home Assistant; select the correct NOVA App from the sidebar.",
		)
	}
	renderSetupLink(out, "This will open:", appTarget.URL)
	if _, err := promptWizardLineFromReader(
		reader,
		out,
		"Press Enter to open NOVA",
		"",
	); err != nil {
		return err
	}
	openAnnouncedBrowserURL(out, appTarget.URL)
	return nil
}

func renderOptionalCloudAddNextStep(
	out io.Writer,
	cfg runtimeConfig,
	serviceMode bool,
) {
	if serviceMode ||
		cfg.Cloud != nil ||
		strings.TrimSpace(cfg.RelaySecureBaseURL) == "" ||
		strings.TrimSpace(cfg.RelaySpkiPin) == "" ||
		!cloudRemoteFeatureAvailable() ||
		cloudCoordinatorForSetup == nil ||
		!cloudCoordinatorForSetup.Available() {
		return
	}
	renderSetupParagraph(
		out,
		"Away-from-home access is optional and not configured.",
		"Add it later with: "+cloudFreshAddCommand(),
	)
}
