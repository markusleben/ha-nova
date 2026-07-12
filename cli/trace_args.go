package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

func parseTraceLatestArgs(args []string) (string, bool, error) {
	return parseTraceEntityArgs("trace latest", args)
}

func parseTraceGetArgs(args []string) (string, string, bool, error) {
	fs := flag.NewFlagSet("trace get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var positional []string
	for _, arg := range args {
		if arg == "--json" || arg == "-json" {
			if err := fs.Set("json", "true"); err != nil {
				return "", "", false, err
			}
			continue
		}
		if arg == "--help" || arg == "-h" {
			_ = helpRequested(flag.ErrHelp, fs, "ha-nova trace get <automation.entity_id|script.entity_id> <run_id> [--json]")
			return "", "", false, errHelpShown
		}
		if strings.HasPrefix(arg, "-") {
			return "", "", false, fmt.Errorf("unknown trace get flag: %s", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 2 {
		return "", "", false, fmt.Errorf("Usage: ha-nova trace get <automation.entity_id|script.entity_id> <run_id> [--json]")
	}
	return strings.TrimSpace(positional[0]), strings.TrimSpace(positional[1]), *jsonOut, nil
}

func parseTraceEntityArgs(command string, args []string) (string, bool, error) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "print machine-readable JSON")
	var positional []string
	for _, arg := range args {
		if arg == "--json" || arg == "-json" {
			if err := fs.Set("json", "true"); err != nil {
				return "", false, err
			}
			continue
		}
		if arg == "--help" || arg == "-h" {
			_ = helpRequested(flag.ErrHelp, fs, fmt.Sprintf("ha-nova %s <automation.entity_id|script.entity_id> [--json]", command))
			return "", false, errHelpShown
		}
		if strings.HasPrefix(arg, "-") {
			return "", false, fmt.Errorf("unknown %s flag: %s", command, arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 1 {
		return "", false, fmt.Errorf("Usage: ha-nova %s <automation.entity_id|script.entity_id> [--json]", command)
	}
	return strings.TrimSpace(positional[0]), *jsonOut, nil
}

func traceDomainFromEntityID(entityID string) (string, bool) {
	switch {
	case strings.HasPrefix(entityID, "automation.") && len(entityID) > len("automation."):
		return "automation", true
	case strings.HasPrefix(entityID, "script.") && len(entityID) > len("script."):
		return "script", true
	default:
		return "", false
	}
}
