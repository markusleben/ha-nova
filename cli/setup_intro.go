package main

import "io"

// renderSetupIntro explains what HA NOVA is and how the wizard behaves before
// the first prompt. Shown only on fully interactive first-time runs (no
// target/flags, no saved Home Assistant address); resumes and flag-driven
// runs skip it.
func renderSetupIntro(out io.Writer) {
	renderSetupParagraph(out,
		setupT("setup.intro.welcome"),
	)
	renderSetupParagraph(out,
		setupT("setup.intro.steps"),
		setupT("setup.intro.browser"),
		setupT("setup.intro.owner"),
		setupT("setup.intro.link_only"),
	)
}
