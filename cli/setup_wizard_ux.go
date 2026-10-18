package main

import (
	"bufio"
	"fmt"
	"io"
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
			"In NOVA (Home Assistant sidebar), click \"Connect a device\" for a one-time six-digit code.",
			"Each computer gets its own connection; remove it in NOVA anytime.",
		)
	} else {
		renderSetupParagraph(out,
			"In NOVA (Home Assistant sidebar), click \"Connect a device\" for a one-time six-digit code.",
			"Each computer gets its own connection; remove it in NOVA anytime.",
			"Development build: the link opens Home Assistant; pick the right NOVA App in the sidebar.",
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
