package main

import "io"

// renderSetupIntro explains what HA NOVA is and how the wizard behaves before
// the first prompt. Shown only on fully interactive runs (no target/flags);
// power users driving setup with arguments skip it.
func renderSetupIntro(out io.Writer) {
	renderSetupParagraph(out,
		"Welcome! HA NOVA connects your AI assistant (like Claude Code) to Home Assistant,",
		"so you can control your smart home and manage automations by simply asking.",
	)
	renderSetupIndentedBlock(out, "This setup walks you through:", "    ",
		"1. Finding your Home Assistant on the network",
		"2. Installing the NOVA Relay app in Home Assistant",
		"3. Setting up two access tokens (guided, step by step)",
		"4. Verifying the connection and installing the skills",
	)
	renderSetupParagraphTight(out,
		"How it works: at several points you'll be asked to press Enter — that opens a",
		"page in your web browser. Do the step there, then come back to this window.",
	)
	renderSetupParagraph(out, "You'll need: Home Assistant running on your network, and an admin login for it.")
}
