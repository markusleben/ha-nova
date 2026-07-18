package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Version is set by goreleaser via ldflags.
var Version = "dev"

// BuildChannel and BuildStamp are injected via -ldflags by scripts/dev-sync.sh
// for locally rebuilt dev binaries. Released builds leave them empty, so
// `ha-nova version` prints only the bare version. This lets any client's LLM be
// asked "which build is loaded?" and answer dev-vs-release deterministically,
// without touching skill files — so it works for symlinked and copied skill
// installs alike.
var (
	BuildChannel = ""
	BuildStamp   = ""
)

func main() {
	paths, err := detectPaths()
	if err != nil {
		printErr("%s", err)
		os.Exit(1)
	}

	argv0 := filepath.Base(os.Args[0])
	exitCode := dispatch(paths, argv0, os.Args[1:])
	os.Exit(exitCode)
}

func dispatch(paths runtimePaths, argv0 string, args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	switch args[0] {
	case "setup":
		return runSetup(paths, args[1:])
	case "doctor":
		return runDoctor(paths, args[1:])
	case "check-update":
		return runCheckUpdate(paths, args[1:])
	case "status":
		return runStatus(paths, args[1:])
	case "update":
		return runUpdate(paths, args[1:])
	case "uninstall":
		return runUninstall(paths, args[1:])
	case "pair":
		return runPairCommand(paths, args[1:])
	case "relay":
		return runRelayCommand(paths, args[1:])
	case "trace":
		return runTraceCommand(paths, args[1:])
	case "snapshot":
		return runSnapshotCommand(paths, args[1:])
	case "diff":
		return runDiffCommand(paths, args[1:])
	case "version":
		if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
			fmt.Fprintln(os.Stdout, "Usage: ha-nova version")
			fmt.Fprintln(os.Stdout, "Prints the installed HA NOVA version. No flags.")
			return 0
		}
		fmt.Fprintln(os.Stdout, versionDisplay(paths))
		return 0
	case "internal-replace":
		return runInternalReplace(paths, args[1:])
	case "internal-uninstall":
		return runInternalUninstall(paths, args[1:])
	case "internal-sync-clients":
		return runInternalSyncClients(paths, args[1:])
	case "internal-setup-readiness":
		return runInternalSetupReadiness(paths, args[1:])
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		printErr("Unknown command: %s", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintln(os.Stdout, "HA NOVA")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  ha-nova setup [client]")
	fmt.Fprintln(os.Stdout, "  ha-nova setup --service [client]")
	fmt.Fprintln(os.Stdout, "  ha-nova doctor [--auto-repair] [--quiet]")
	fmt.Fprintln(os.Stdout, "  ha-nova check-update [--quiet] [--json]")
	fmt.Fprintln(os.Stdout, "  ha-nova status --json")
	fmt.Fprintln(os.Stdout, "  ha-nova update [--version <tag>] [--force]")
	fmt.Fprintln(os.Stdout, "  ha-nova uninstall [--yes] [--purge]")
	fmt.Fprintln(os.Stdout, "  ha-nova relay <health|ws|core|jq|version>")
	fmt.Fprintln(os.Stdout, "  ha-nova trace <latest|list|get> <automation.entity_id|script.entity_id> [run_id] [--json]")
	fmt.Fprintln(os.Stdout, "  ha-nova snapshot <save|show|verify>")
	fmt.Fprintln(os.Stdout, "  ha-nova diff --before <file> --after <file> [--out <file>]")
	fmt.Fprintln(os.Stdout, "  ha-nova version")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Run 'ha-nova <command> --help' to see every flag of a command.")
}
