package main

import (
	"bufio"
	"fmt"
	"io"
)

// Typed views the setup engine hands to a renderer. The text renderer below
// reproduces the terminal wizard; the setup page renders the same views.
type setupView struct {
	Progress *setupProgress
	Blocks   []setupBlock
}

type setupProgress struct {
	Step, Total int
	Title       setupMsg
}

type setupBlockKind uint8

const (
	setupBlockParagraph setupBlockKind = iota // blank line, then the lines
	setupBlockTight                           // the lines, no blank line
	setupBlockList                            // a paragraph title, then indented items
	setupBlockSuccess                         // one status line each
	setupBlockError
	setupBlockLink  // a label and a URL (data)
	setupBlockBreak // one blank line
)

type setupBlock struct {
	Kind  setupBlockKind
	Lines []setupMsg
	Items []setupMsg
	URL   string
}

func setupParagraph(lines ...setupMsg) setupBlock {
	return setupBlock{Kind: setupBlockParagraph, Lines: lines}
}

func setupTight(lines ...setupMsg) setupBlock {
	return setupBlock{Kind: setupBlockTight, Lines: lines}
}

func setupList(title setupMsg, items ...setupMsg) setupBlock {
	return setupBlock{Kind: setupBlockList, Lines: []setupMsg{title}, Items: items}
}

func setupSuccess(line setupMsg) setupBlock {
	return setupBlock{Kind: setupBlockSuccess, Lines: []setupMsg{line}}
}

func setupErrorBlock(line setupMsg) setupBlock {
	return setupBlock{Kind: setupBlockError, Lines: []setupMsg{line}}
}

var setupBreak = setupBlock{Kind: setupBlockBreak}

func setupLink(label setupMsg, url string) setupBlock {
	return setupBlock{Kind: setupBlockLink, Lines: []setupMsg{label}, URL: url}
}

// setupOutcome is the end screen of a setup run.
type setupOutcomeKind uint8

const (
	setupOutcomeComplete    setupOutcomeKind = iota // clients installed
	setupOutcomeLinkReady                           // local plus Home Assistant Link fallback
	setupOutcomeAlreadyDone                         // nothing to do
	setupOutcomeIncomplete                          // saved, but not ready
	setupOutcomeLinkPaused                          // local ready, Home Assistant Link waits for the owner
)

type setupOutcome struct {
	Kind        setupOutcomeKind
	Clients     []string
	Look        *setupFirstLook
	LegacyToken bool
	Issue       string
	LinkSteps   string // setupOutcomeLinkPaused: its commands
}

// setupUI is what the setup engine asks of a renderer: the terminal
// (setupTextUI) or the local setup page (setupWebUI, setup_page_ui.go).
type setupUI interface {
	header()
	show(view setupView)
	choose(ask setupAsk) (string, error)
	listChoices(ask setupAsk)
	openURL(url string)
	interactive() bool
	input(label setupMsg) (string, error)
	working(label setupMsg, task func() error) error
	outcome(o setupOutcome)
	note(line string)
	cancelled()
	fail(msg setupMsg)
	failErr(err error)
	info(msg setupMsg)
	// termIn and termOut serve the steps that stay text-only (recovery
	// edge states, spec §2). On the setup page, reading termIn hands
	// setup over to the terminal.
	termIn() *bufio.Reader
	termOut() io.Writer
}

// setupTextUI renders setup views in the terminal.
type setupTextUI struct {
	reader *bufio.Reader
	out    io.Writer
}

func (ui setupTextUI) termIn() *bufio.Reader { return ui.reader }

func (ui setupTextUI) termOut() io.Writer { return ui.out }

func (ui setupTextUI) header() {
	renderSetupHeader(ui.out)
}

func (ui setupTextUI) show(view setupView) {
	if view.Progress != nil {
		renderSetupStep(ui.out, view.Progress.Step, view.Progress.Total, view.Progress.Title.plain())
	}
	for _, block := range view.Blocks {
		lines := make([]string, len(block.Lines))
		for i, line := range block.Lines {
			lines[i] = line.plain()
		}
		switch block.Kind {
		case setupBlockParagraph:
			renderSetupParagraph(ui.out, lines...)
		case setupBlockTight:
			renderSetupParagraphTight(ui.out, lines...)
		case setupBlockList:
			items := make([]string, len(block.Items))
			for i, item := range block.Items {
				items[i] = item.plain()
			}
			renderSetupIndentedBlock(ui.out, lines[0], "    ", items...)
		case setupBlockSuccess:
			for _, line := range lines {
				renderSetupSuccessLine(ui.out, "%s", line)
			}
		case setupBlockError:
			for _, line := range lines {
				renderSetupErrorLine(ui.out, "%s", line)
			}
		case setupBlockLink:
			renderSetupLink(ui.out, lines[0], block.URL)
		case setupBlockBreak:
			fmt.Fprintln(ui.out)
		}
	}
}

func (ui setupTextUI) choose(ask setupAsk) (string, error) {
	return promptSetupChoice(ui.reader, ui.out, ask)
}

// listChoices shows the choices without asking (nothing to pick).
func (ui setupTextUI) listChoices(ask setupAsk) {
	renderSetupChoiceList(ui.out, ask)
}

// openURL opens a link the view just announced.
func (ui setupTextUI) openURL(url string) {
	openAnnouncedBrowserURL(ui.out, url)
}

// interactive: a person can answer optional offers here.
func (ui setupTextUI) interactive() bool {
	return isInteractiveTTY() && writerSupportsTTYForSetup(ui.out)
}

// input asks for one line; errSetupBack and errSetupExit come back typed.
func (ui setupTextUI) input(label setupMsg) (string, error) {
	return promptWizardLineFromReader(ui.reader, ui.out, label.plain(), "")
}

// working shows a progress label while task runs.
func (ui setupTextUI) working(label setupMsg, task func() error) error {
	return runSetupStepWithFeedback(ui.out, label.plain(), task)
}

func (ui setupTextUI) outcome(o setupOutcome) {
	switch o.Kind {
	case setupOutcomeLinkReady:
		renderSetupCloudFallbackReadyBanner(ui.out, o.Look)
	case setupOutcomeAlreadyDone:
		renderSetupAlreadyDoneBanner(ui.out, o.LegacyToken)
	case setupOutcomeIncomplete:
		renderSetupIncompleteBanner(ui.out, o.Issue)
	case setupOutcomeLinkPaused:
		renderSetupCloudPausedOutcome(ui.out, o.LinkSteps)
	default:
		renderSetupCompleteBanner(ui.out, o.Clients, o.Look)
	}
}

// note prints a line that belongs after the outcome (e.g. the Claude rule).
func (ui setupTextUI) note(line string) {
	fmt.Fprintln(ui.out, "  "+line)
}

func (ui setupTextUI) cancelled() {
	renderSetupCancelledNote(ui.out)
}

func (ui setupTextUI) fail(msg setupMsg) {
	printHumanErr("%s", msg.plain())
}

func (ui setupTextUI) failErr(err error) {
	printHumanErr("%s", err)
}

func (ui setupTextUI) info(msg setupMsg) {
	printHumanInfo("%s", msg.plain())
}
