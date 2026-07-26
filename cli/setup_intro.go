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
		"- ask whether this computer should connect locally or through Home Assistant Cloud",
		"- for local setup: find Home Assistant and install the NOVA App",
		"- for Cloud-only setup: connect to an existing NOVA App through your Cloud Remote URL",
		"- set up two access tokens (guided, step by step)",
		`- verify the connection and teach your AI assistant the Home Assistant commands (the "skills")`,
	)
	renderSetupParagraph(out,
		"How it works: at several points you'll be asked to press Enter — that opens a",
		"page in your web browser. Do the step there, then come back to this window.",
	)
	renderSetupParagraph(out,
		"You'll need: either local Home Assistant administrator access, or an active Home Assistant Cloud Remote connection with the NOVA App already installed.",
		"Cloud-only setup also requires a Home Assistant Owner to approve this device in the NOVA App.",
	)
}
