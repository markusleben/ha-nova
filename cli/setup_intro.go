package main

import "io"

// renderSetupIntro explains what HA NOVA is and how the wizard behaves before
// the first prompt. Shown only on fully interactive first-time runs (no
// target/flags, no saved Home Assistant address); resumes and flag-driven
// runs skip it.
func renderSetupIntro(out io.Writer) {
	renderSetupParagraph(out,
		"Welcome! HA NOVA lets your AI assistant run your Home Assistant.",
	)
	renderSetupParagraph(out,
		"Steps: pick your AI client, find Home Assistant, add the NOVA App, connect this computer, install the skills.",
		"When a browser page opens, finish there and come back here.",
		"You need the Home Assistant owner's account.",
		"Home Assistant Link only: the NOVA App must already be installed.",
	)
}
