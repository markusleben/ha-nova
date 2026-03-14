package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Version is set by goreleaser via ldflags.
var Version = "dev"

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
	case "update":
		return runUpdate(paths, args[1:])
	case "uninstall":
		return runUninstall(paths, args[1:])
	case "relay":
		return runRelayCommand(paths, args[1:])
	case "version":
		fmt.Fprintln(os.Stdout, localVersion(paths))
		return 0
	case "internal-replace":
		return runInternalReplace(paths, args[1:])
	case "internal-sync-clients":
		return runInternalSyncClients(paths, args[1:])
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
	fmt.Fprintln(os.Stdout, "  ha-nova doctor")
	fmt.Fprintln(os.Stdout, "  ha-nova check-update [--quiet]")
	fmt.Fprintln(os.Stdout, "  ha-nova update [--version <tag>]")
	fmt.Fprintln(os.Stdout, "  ha-nova uninstall [--yes]")
	fmt.Fprintln(os.Stdout, "  ha-nova relay <health|ws|core|jq|version>")
	fmt.Fprintln(os.Stdout, "  ha-nova version")
}
