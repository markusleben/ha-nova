package main

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// helpRequested reports whether fs.Parse ended in --help/-h and, if so,
// prints the usage line plus the registered flag defaults on stdout. Every
// subcommand funnels through this so `ha-nova <command> --help` is a
// first-class answer (exit 0), not the former "ERROR: flag: help requested".
// Callers return 0 when it reports true and keep their own error path
// otherwise.
// errHelpShown signals that --help was already answered on stdout by a
// nested parser; callers translate it to exit code 0 without printing more.
var errHelpShown = errors.New("help shown")

func helpRequested(err error, fs *flag.FlagSet, usageLine string) bool {
	if !errors.Is(err, flag.ErrHelp) {
		return false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s\n\nFlags:\n", usageLine)
	fs.SetOutput(&b)
	fs.PrintDefaults()
	fmt.Print(b.String())
	return true
}
