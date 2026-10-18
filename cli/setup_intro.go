package main

import "io"

// renderSetupIntro explains what HA NOVA is and how the wizard behaves before
// the first prompt. Shown only on fully interactive first-time runs (no
// target/flags, no saved Home Assistant address); resumes and flag-driven
// runs skip it.
func renderSetupIntro(out io.Writer) {
	renderSetupParagraph(out,
		"Welcome! HA NOVA connects your AI assistant (like Claude Code) to Home Assistant,",
		"so you can control your smart home and manage automations by simply asking.",
	)
	renderSetupIndentedBlock(out, "This setup will:", "    ",
		"- ask which AI client you use",
		"- ask whether this computer should connect locally or through Home Assistant Link (formerly Home Assistant Cloud)",
		"- for local setup: find Home Assistant and install the NOVA App",
		"- for Home Assistant Link only: connect to an existing NOVA App through your Home Assistant Link remote URL",
		"- pair this computer: you sign in to Home Assistant as the owner, or type a one-time six-digit code from the NOVA page (each device gets its own connection you can revoke anytime)",
		`- verify the connection and teach your AI assistant the Home Assistant commands (the "skills")`,
	)
	renderSetupParagraph(out,
		"How it works: at several points you'll be asked to press Enter — that opens a",
		"page in your web browser. Do the step there, then come back to this window.",
	)
	renderSetupParagraph(out,
		"You'll need: either local Home Assistant administrator access, or active Home Assistant Link remote access with the NOVA App already installed.",
		"Setup with Home Assistant Link only also requires a Home Assistant Owner to approve this device in the NOVA App.",
	)
}
