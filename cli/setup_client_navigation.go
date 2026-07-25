package main

import (
	"bufio"
	"io"
)

func promptSetupClientForWizard(
	reader *bufio.Reader,
	out io.Writer,
	choices []setupClientChoice,
) (string, error) {
	for {
		target, err := promptSetupClientInteractive(
			reader,
			out,
			choices,
			"claude",
		)
		if err == errSetupBack {
			continue
		}
		return target, err
	}
}
