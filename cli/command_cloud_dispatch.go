package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type cloudCommandFlags struct {
	server                     string
	url                        string
	json                       bool
	yes                        bool
	remoteResume               bool
	confirmRemoteAccessRevoked string
}

func runCloudCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printCloudUsage()
		return 1
	}
	switch args[0] {
	case "add":
		return runCloudConnectCommand(paths, args[1:], false)
	case "reconnect":
		return runCloudConnectCommand(paths, args[1:], true)
	case "status":
		return runCloudStatusCommand(paths, args[1:])
	case "unlock":
		return runCloudUnlockCommand(paths, args[1:])
	case "remove":
		return runCloudRemoveCommand(paths, args[1:])
	case "-h", "--help", "help":
		printCloudUsage()
		return 0
	default:
		printHumanErr("unknown cloud command: %s", args[0])
		printCloudUsage()
		return 1
	}
}

func printCloudUsage() {
	fmt.Fprintln(
		os.Stdout,
		"Usage: ha-nova cloud <add|status|unlock|reconnect|remove>",
	)
	fmt.Fprintln(os.Stdout, "  add [--server <name>] [--url https://…] [--remote-resume]")
	fmt.Fprintln(os.Stdout, "  status [--server <name>] [--json]")
	fmt.Fprintln(
		os.Stdout,
		"  unlock [--server <name>]     Show the native secure-storage prompt",
	)
	fmt.Fprintln(
		os.Stdout,
		"  reconnect [--server <name>] [--url https://…] [--remote-resume]  Rotate the Home Assistant authorization",
	)
	fmt.Fprintln(
		os.Stdout,
		"  remove [--server <name>] [--yes] [--confirm-remote-access-revoked <name>]",
	)
}

func parseCloudCommandFlags(
	command string,
	args []string,
) (cloudCommandFlags, error) {
	var result cloudCommandFlags
	fs := flag.NewFlagSet("cloud "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&result.server, "server", "", "server profile")
	if command == "add" || command == "reconnect" {
		fs.StringVar(
			&result.url,
			"url",
			"",
			"Home Assistant Cloud URL for remote-first setup",
		)
	}
	if command == "add" || command == "reconnect" {
		fs.BoolVar(
			&result.remoteResume,
			"remote-resume",
			false,
			"resume a checkpoint at cloud_verified or later from a remote (SSH) session",
		)
	}
	if command == "status" {
		fs.BoolVar(&result.json, "json", false, "print JSON")
	}
	if command == "remove" {
		fs.BoolVar(&result.yes, "yes", false, "skip confirmation")
		fs.StringVar(
			&result.confirmRemoteAccessRevoked,
			"confirm-remote-access-revoked",
			"",
			"confirm manual revocation for this exact server profile",
		)
	}
	if err := fs.Parse(args); err != nil {
		if helpRequested(
			err,
			fs,
			"ha-nova cloud "+command+" [--server <name>]",
		) {
			return result, errHelpShown
		}
		return result, err
	}
	if fs.NArg() != 0 {
		return result, fmt.Errorf(
			"cloud %s does not accept positional arguments",
			command,
		)
	}
	if strings.TrimSpace(result.server) != result.server {
		return result, errors.New(
			"--server must not have leading or trailing whitespace",
		)
	}
	if result.server != "" {
		if err := validateServerProfileName(result.server); err != nil {
			return result, err
		}
		setServerSelectionOverride(result.server)
	}
	if strings.TrimSpace(result.url) != result.url {
		return result, errors.New(
			"--url must not have leading or trailing whitespace",
		)
	}
	if strings.TrimSpace(result.confirmRemoteAccessRevoked) !=
		result.confirmRemoteAccessRevoked {
		return result, errors.New(
			"--confirm-remote-access-revoked must not have leading or trailing whitespace",
		)
	}
	if result.confirmRemoteAccessRevoked != "" {
		if err := validateServerProfileName(
			result.confirmRemoteAccessRevoked,
		); err != nil {
			return result, fmt.Errorf(
				"invalid --confirm-remote-access-revoked value: %w",
				err,
			)
		}
	}
	return result, nil
}
