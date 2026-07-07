package main

import (
	"fmt"
	"io"
)

func renderSetupParagraph(out io.Writer, lines ...string) {
	renderSetupParagraphWithGap(out, true, lines...)
}

func renderSetupParagraphTight(out io.Writer, lines ...string) {
	renderSetupParagraphWithGap(out, false, lines...)
}

func renderSetupParagraphWithGap(out io.Writer, leadingGap bool, lines ...string) {
	if len(lines) == 0 {
		return
	}
	if leadingGap {
		fmt.Fprintln(out)
	}
	for _, line := range lines {
		fmt.Fprintf(out, "  %s\n", line)
	}
}

func renderSetupSectionTitle(out io.Writer, title string) {
	if title == "" {
		return
	}
	session := resolveSetupUISession(out)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", title))
}

func renderSetupIndentedLines(out io.Writer, indent string, lines ...string) {
	for _, line := range lines {
		fmt.Fprintf(out, "%s%s\n", indent, line)
	}
}

func renderSetupIndentedBlock(out io.Writer, title, indent string, lines ...string) {
	if title != "" {
		renderSetupParagraph(out, title)
	} else {
		fmt.Fprintln(out)
	}
	renderSetupIndentedLines(out, indent, lines...)
}

// renderSetupLink prints a label with its URL on an own indented line, so a
// long URL wraps at a clean boundary instead of mid-sentence.
func renderSetupLink(out io.Writer, label, target string) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", label)
	fmt.Fprintf(out, "      %s\n", target)
}

func renderSetupCancelledNote(out io.Writer) {
	renderSetupParagraph(out, "Setup cancelled.")
}

func renderSetupStatusLine(out io.Writer, role, format string, args ...interface{}) {
	session := resolveSetupUISession(out)
	message := fmt.Sprintf(format, args...)
	marker := ""
	switch role {
	case "success":
		marker = session.successMarker()
	case "warning":
		marker = session.warningMarker()
	default:
		role = "error"
		marker = session.errorMarker()
	}
	fmt.Fprintf(out, "  %s %s\n", session.style(role, marker), message)
}

func renderSetupMutedNoteBlock(out io.Writer, title string, lines ...string) {
	session := resolveSetupUISession(out)
	fmt.Fprintln(out)
	if title != "" {
		fmt.Fprintf(out, "  %s\n", session.style("muted", "[ "+title+" ]"))
	}
	for _, line := range lines {
		fmt.Fprintf(out, "  %s\n", session.style("muted", "  "+line))
	}
}

func renderSetupSuccessLine(out io.Writer, format string, args ...interface{}) {
	renderSetupStatusLine(out, "success", format, args...)
}

func renderSetupWarningLine(out io.Writer, format string, args ...interface{}) {
	renderSetupStatusLine(out, "warning", format, args...)
}

func renderSetupErrorLine(out io.Writer, format string, args ...interface{}) {
	renderSetupStatusLine(out, "error", format, args...)
}
