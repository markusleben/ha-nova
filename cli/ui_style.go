package main

import (
	"fmt"
	"io"
	"strings"
)

func formatMessage(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

func writeStyledLine(out io.Writer, prefix, message string) {
	if strings.TrimSpace(prefix) == "" {
		fmt.Fprintln(out, message)
		return
	}
	fmt.Fprintf(out, "%s %s\n", prefix, message)
}

func renderSimpleHeader(out io.Writer, session uiSession, title string) {
	fmt.Fprintln(out)
	if session.plain() {
		fmt.Fprintf(out, "  %s\n", title)
		fmt.Fprintf(out, "  %s\n", strings.Repeat("-", len(title)))
		fmt.Fprintln(out)
		return
	}

	fmt.Fprintf(out, "  %s\n", session.style("accent", title))
	fmt.Fprintf(out, "  %s\n", session.style("accent", strings.Repeat("─", len(title))))
	fmt.Fprintln(out)
}
