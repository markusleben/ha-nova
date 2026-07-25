package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type cloudCommandFlags struct {
	server string
	url    string
	json   bool
	yes    bool
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
	fmt.Fprintln(os.Stdout, "Usage: ha-nova cloud <add|status|unlock|reconnect|remove>")
	fmt.Fprintln(os.Stdout, "  add [--server <name>] [--url https://…]")
	fmt.Fprintln(os.Stdout, "  status [--server <name>] [--json]")
	fmt.Fprintln(os.Stdout, "  unlock [--server <name>]     Show the native secure-storage prompt")
	fmt.Fprintln(os.Stdout, "  reconnect [--server <name>] [--url https://…]  Rotate the Home Assistant authorization")
	fmt.Fprintln(os.Stdout, "  remove [--server <name>] [--yes]")
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
	if command == "status" {
		fs.BoolVar(&result.json, "json", false, "print JSON")
	}
	if command == "remove" {
		fs.BoolVar(&result.yes, "yes", false, "skip confirmation")
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
		return result, fmt.Errorf("cloud %s does not accept positional arguments", command)
	}
	if strings.TrimSpace(result.server) != result.server {
		return result, errors.New("--server must not have leading or trailing whitespace")
	}
	if result.server != "" {
		if err := validateServerProfileName(result.server); err != nil {
			return result, err
		}
		setServerSelectionOverride(result.server)
	}
	if strings.TrimSpace(result.url) != result.url {
		return result, errors.New("--url must not have leading or trailing whitespace")
	}
	return result, nil
}

func runCloudUnlockCommand(paths runtimePaths, args []string) int {
	options, err := parseCloudCommandFlags("unlock", args)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if !cloudInteractivePromptSessionForSetup() {
		printHumanErr(
			"cloud unlock requires an interactive desktop session: use a local, non-elevated graphical desktop terminal (not SSH, sudo/root, or WSL)",
		)
		return 1
	}
	cfg, preProfile, err := loadCloudUnlockConfig(paths, options)
	if err != nil {
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = withCloudSecretAccessHolder(ctx)
	if err := preflightCloudUnlockDeviceAccess(ctx, cfg, preProfile); err != nil {
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	if preProfile || (cfg.ProfileID == "" && cfg.Cloud == nil) {
		if !cloudRemoteFeatureAvailable() {
			printHumanInfo(
				"Device credential storage was checked. Cloud transport is disabled in this build.",
			)
			printHumanInfo(
				"No Cloud checkpoint was saved, so no Cloud cleanup is needed. Local-only setup remains available.",
			)
			return 0
		}
		printHumanInfo(
			"Device credential storage is unlocked. No Cloud checkpoint was saved.",
		)
		printHumanInfo(
			"OAuth storage has no profile-scoped slot yet and will be checked during guided Cloud setup.",
		)
		printHumanInfo(
			"Start guided Cloud setup with: %s",
			cloudFreshAddCommand(),
		)
		return 0
	}
	ctx, err = preflightCloudSecretAccessSession(
		ctx,
		cloudCoordinatorForSetup,
		cfg,
		cloudSecretPreflightUnlock,
	)
	if err != nil {
		renderCloudFailure(os.Stdout, paths, err)
		return 1
	}
	var verifiedStorageHold *cloudRecoveryHold
	if cfg.Cloud != nil && cfg.Cloud.RecoveryHold != nil {
		hold := *cfg.Cloud.RecoveryHold
		if cloudRecoveryHoldClearsAfterUnlock(&hold) {
			verifiedStorageHold = &hold
		} else {
			printHumanInfo(
				"Native secure storage is unlocked, but Cloud recovery remains paused for security review.",
			)
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
			return 0
		}
	}
	if !cloudRemoteFeatureAvailable() {
		if verifiedStorageHold != nil {
			printHumanInfo(
				"Native secure storage is unlocked, but the recovery safety hold remains until Cloud health can be verified.",
			)
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
			return 0
		}
		printHumanInfo(
			"Native secure storage is unlocked for cleanup; Cloud transport remains disabled in this build.",
		)
		if cfg.Cloud != nil {
			printHumanInfo(
				"Verified cleanup remains available with: %s",
				cloudRemoveCommand(),
			)
		}
		return 0
	}
	if cfg.Cloud == nil {
		printHumanInfo(
			"Native secure storage is unlocked. Start guided Cloud setup with: %s",
			cloudFreshAddCommand(),
		)
		return 0
	}
	if cfg.Cloud.ready() {
		snapshot, healthErr := loadAndVerifyCloudHealthWithCheckpoint(
			ctx,
			paths,
			verifyCloudDeviceHealthForCommand,
			verifiedStorageHold,
		)
		if healthErr != nil {
			renderCloudFailure(os.Stdout, paths, healthErr)
			return 1
		}
		if verifiedStorageHold != nil {
			expected, expectationErr := snapshot.recoveryExpectation()
			if expectationErr != nil {
				renderCloudFailure(os.Stdout, paths, expectationErr)
				return 1
			}
			cfg, err = clearCloudRecoveryHoldAtSnapshot(
				paths,
				expected,
				*verifiedStorageHold,
			)
			if err != nil {
				renderCloudFailure(os.Stdout, paths, err)
				return 1
			}
			printVerifiedStorageHoldCleared()
		}
		printHumanInfo("Native secure storage is unlocked and Cloud access is ready.")
		return 0
	}
	if verifiedStorageHold != nil {
		printHumanInfo(
			"Native secure storage is unlocked, but the recovery safety hold remains until Cloud access is ready and health has been verified.",
		)
		printHumanInfo(
			"Verified cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return 0
	}
	if cfg.Cloud.Current != nil {
		printHumanInfo(
			"Native secure storage is unlocked; a Cloud update is waiting at %q.",
			cfg.Cloud.State,
		)
	} else {
		printHumanInfo(
			"Native secure storage is unlocked; Cloud setup is waiting at %q.",
			cfg.Cloud.State,
		)
	}
	printHumanInfo("Resume with: %s", cloudResumeCommand(cfg))
	return 0
}

func printVerifiedStorageHoldCleared() {
	printHumanInfo(
		"Native secure storage was verified; the recovery safety hold was cleared.",
	)
}

func loadCloudManagementConfig(paths runtimePaths) (runtimeConfig, error) {
	snapshot, err := loadCloudManagementSnapshot(paths)
	return snapshot.Config, err
}

func printCloudCommandProblem(err error) *cloudProblem {
	problem := cloudProblemForCommandError(err)
	printHumanErr("%s", problem)
	if problem.Remediation == cloudRemediationUnlockStorage {
		printHumanInfo(
			"From a local, non-elevated graphical desktop terminal, run: %s",
			cloudUnlockCommand(),
		)
	}
	return problem
}

func cloudProblemForCommandError(err error) *cloudProblem {
	problem := cloudProblemForError(err)
	if isDesktopKeyringSetupRequiredError(err) ||
		isDesktopKeyringSessionUnavailableError(err) ||
		isDesktopKeyringUnavailableError(err) ||
		isWindowsNetworkLogonSessionError(err) {
		problem = &cloudProblem{
			Code:        cloudProblemSecureStorage,
			Remediation: cloudRemediationUnlockStorage,
			Detail:      "native secure storage is locked or unavailable in this session",
			Cause:       err,
		}
	}
	return problem
}

func verifyCloudDeviceHealth(ctx context.Context, cfg runtimeConfig) error {
	selection, err := resolveCloudRelayTransport(ctx, cfg)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(selection.BaseURL, "/")+"/health",
		nil,
	)
	if err != nil {
		return newCloudError(CloudErrInvalidInput, "build Cloud health request", err)
	}
	request.Header.Set("Authorization", "Bearer "+selection.Credential)
	request.Header.Set("Accept", "application/json")
	response, err := selection.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := readCloudResponse(
		response.Body,
		cloudLocalDiscoveryMaxBytes,
		"read Cloud health response",
	)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return newCloudHTTPError(
			CloudErrDeviceRejected,
			"verify Cloud device",
			response.StatusCode,
			false,
		)
	}
	return validateCloudRelayHealthIdentity(body, cfg.RelayInstanceID)
}
