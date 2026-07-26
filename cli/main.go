package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if handled, exitCode := maybeRunNativeSecretWorker(
		os.Args[1:],
		os.Stdin,
		os.Stdout,
	); handled {
		os.Exit(exitCode)
	}
	paths, err := detectPaths()
	if err != nil {
		printErr("%s", err)
		os.Exit(1)
	}
	configureCloudRemoteFeature(paths)

	argv0 := filepath.Base(os.Args[0])
	exitCode := dispatch(paths, argv0, os.Args[1:])
	os.Exit(exitCode)
}

func dispatch(paths runtimePaths, argv0 string, args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}
	if err := recoverConfigTransactionBeforeDispatch(paths); err != nil {
		if printCloudStatusJSONForDispatchRecoveryFailure(
			paths,
			args,
			err,
		) {
			return 1
		}
		printErr(
			"HA NOVA cannot safely recover an interrupted configuration update: %s",
			err,
		)
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
	case "server":
		return runServerCommand(paths, args[1:])
	case "cloud":
		return runCloudCommand(paths, args[1:])
	case "relay":
		return runRelayCommand(paths, args[1:])
	case "trace":
		return runTraceCommand(paths, args[1:])
	case "snapshot":
		return runSnapshotCommand(paths, args[1:])
	case "census":
		return runCensusCommand(paths, args[1:])
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
	case "internal-cloud-release-check":
		if len(args) != 1 {
			printErr("internal-cloud-release-check accepts no arguments")
			return 1
		}
		identity := cloudRemoteBuildIdentityForRuntime()
		_, platformEnabled := cloudRemoteReleasePlatforms[runtime.GOOS]
		if !identity.Official || !cloudRemoteReleaseEnabled || !platformEnabled {
			printErr("official Cloud release provenance is not enabled")
			return 1
		}
		fmt.Fprintln(os.Stdout, "official Cloud release provenance verified")
		return 0
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		printErr("Unknown command: %s", args[0])
		printUsage()
		return 1
	}
}

func printCloudStatusJSONForDispatchRecoveryFailure(
	paths runtimePaths,
	args []string,
	cause error,
) bool {
	if len(args) < 2 ||
		args[0] != "cloud" ||
		args[1] != "status" {
		return false
	}
	rawIntent := scanCloudStatusArgs(args[2:])
	if !rawIntent.jsonRequested {
		return false
	}
	problem := &cloudProblem{
		Code:        cloudProblemConfigInvalid,
		Remediation: cloudRemediationSecurityStop,
		Detail: "an interrupted configuration update could not be " +
			"safely recovered; Cloud was not contacted",
		Cause: cause,
	}
	printCloudStatusJSON(cloudStatusSummary{
		Status: "error",
		Server: cloudStatusServerForReport(
			paths,
			cloudCommandFlags{},
			rawIntent,
		),
		VerificationError: cloudStatusErrorForProblem(problem),
	})
	return true
}

func recoverConfigTransactionBeforeDispatch(paths runtimePaths) error {
	recoveryNeeded := false
	for _, transactionPath := range []string{
		conditionalJSONTransactionPath(paths.ConfigFile),
		conditionalJSONCommittedTransactionPath(paths.ConfigFile),
	} {
		if _, err := os.Lstat(transactionPath); err == nil {
			recoveryNeeded = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !recoveryNeeded {
		return nil
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return errors.New(
			"another HA NOVA configuration update is in progress",
		)
	}
	defer release()
	return recoverConditionalJSONTransaction(paths.ConfigFile)
}

func printUsage() {
	fmt.Fprintln(os.Stdout, "HA NOVA")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  ha-nova setup [client]")
	fmt.Fprintln(os.Stdout, "  ha-nova setup --service [client]")
	fmt.Fprintln(os.Stdout, "  ha-nova pair [--relay-url http://<ha-host>:8791] [--code NNNNNN] [--credential-store=file]")
	fmt.Fprintln(os.Stdout, "  ha-nova server <list|default|rename|remove|route>")
	fmt.Fprintln(os.Stdout, "  ha-nova cloud <add|status|unlock|reconnect|remove>")
	fmt.Fprintln(os.Stdout, "  ha-nova doctor [--auto-repair] [--quiet]")
	fmt.Fprintln(os.Stdout, "  ha-nova check-update [--quiet] [--json]")
	fmt.Fprintln(os.Stdout, "  ha-nova status --json")
	fmt.Fprintln(os.Stdout, "  ha-nova update [--version <tag>] [--force]")
	fmt.Fprintln(os.Stdout, "  ha-nova uninstall [--yes] [--purge]")
	fmt.Fprintln(os.Stdout, "  ha-nova relay <health|ws|core|files|backups|jq|version>")
	fmt.Fprintln(os.Stdout, "  ha-nova trace <latest|list|get> <automation.entity_id|script.entity_id> [run_id] [--json]")
	fmt.Fprintln(os.Stdout, "  ha-nova snapshot <save|show|verify>")
	fmt.Fprintln(os.Stdout, "  ha-nova census <on|off|status>")
	fmt.Fprintln(os.Stdout, "  ha-nova diff --before <file> --after <file> [--out <file>]")
	fmt.Fprintln(os.Stdout, "  ha-nova version")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Run 'ha-nova <command> --help' to see every flag of a command.")
}
