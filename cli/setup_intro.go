package main

import "io"

// renderSetupIntro explains what HA NOVA is and how the wizard behaves before
// the first prompt. Shown only on fully interactive first-time runs (no
// target/flags, no saved Home Assistant address); resumes and flag-driven
// runs skip it.
func renderSetupIntro(out io.Writer) {
	setupTextUI{out: out}.show(setupIntroView())
}

func setupIntroView() setupView {
	return setupView{Blocks: []setupBlock{
		setupParagraph(setupText("setup.intro.welcome")),
		setupParagraph(
			setupText("setup.intro.steps"),
			setupText("setup.intro.browser"),
			setupText("setup.intro.owner"),
			setupText("setup.intro.link_only"),
		),
	}}
}
