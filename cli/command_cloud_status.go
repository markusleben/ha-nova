package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type cloudStatusSummary struct {
	Status            string              `json:"status"`
	Lifecycle         cloudLifecycleState `json:"lifecycle"`
	Server            string              `json:"server"`
	RoutePolicy       routePolicy         `json:"route_policy"`
	Origin            string              `json:"origin,omitempty"`
	UserBound         bool                `json:"user_bound"`
	CurrentAvailable  bool                `json:"current_available"`
	CurrentReady      bool                `json:"current_ready"`
	Pending           bool                `json:"pending"`
	NextCommand       string              `json:"next_command,omitempty"`
	VerificationError *cloudStatusError   `json:"verification_error,omitempty"`
}

type cloudStatusError struct {
	Code        cloudProblemCode `json:"code"`
	Remediation cloudRemediation `json:"remediation"`
	Detail      string           `json:"detail"`
}

var verifyCloudDeviceHealthForCommand = verifyCloudDeviceHealth

func runCloudStatusCommand(paths runtimePaths, args []string) int {
	rawIntent := scanCloudStatusArgs(args)
	jsonRequested := rawIntent.jsonRequested
	options, err := parseCloudCommandFlags("status", args)
	reportedServer := cloudStatusServerForReport(paths, options, rawIntent)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		if jsonRequested {
			printCloudStatusJSON(cloudStatusSummary{
				Status: "error",
				Server: reportedServer,
				VerificationError: cloudStatusErrorForProblem(
					cloudProblemForError(err),
				),
			})
			return 1
		}
		printHumanErr("%s", err)
		return 1
	}
	options.json = options.json || jsonRequested
	snapshot, err := loadCloudManagementSnapshot(paths)
	if err != nil {
		if cloudStatusHandledInvalidInstallIdentity(
			paths,
			options,
			err,
		) {
			return 1
		}
		if options.json {
			printCloudStatusJSON(cloudStatusSummary{
				Status: "error",
				Server: reportedServer,
				VerificationError: cloudStatusErrorForProblem(
					cloudProblemForError(err),
				),
			})
			return 1
		}
		printHumanErr("%s", err)
		return 1
	}
	cfg := snapshot.Config
	reportedServer = snapshot.ProfileName
	if cfg.Cloud == nil {
		if options.json {
			problem := cloudNotConfiguredProblem()
			if !cloudRemoteFeatureAvailable() {
				problem = cloudAdapterUnavailableProblem()
			}
			summary := cloudStatusSummary{
				Status:      "not_configured",
				Server:      reportedServer,
				RoutePolicy: effectiveRoutePolicy(cfg.RoutePolicy),
				VerificationError: cloudStatusErrorForProblem(
					problem,
				),
			}
			if cloudRemoteFeatureAvailable() {
				summary.NextCommand = cloudResumeCommand(cfg)
			}
			printCloudStatusJSON(summary)
			return 1
		}
		if !cloudRemoteFeatureAvailable() {
			printHumanErr("%s", cloudAdapterUnavailableProblem())
			printHumanInfo(
				"Cloud setup is disabled in this build. No Cloud cleanup is needed for this profile; local-only setup remains available.",
			)
			return 1
		}
		printHumanErr("%s", cloudNotConfiguredProblem())
		return 1
	}
	summary := cloudStatusSummary{
		Status:           "setup_pending",
		Lifecycle:        cfg.Cloud.State,
		Server:           reportedServer,
		RoutePolicy:      effectiveRoutePolicy(cfg.RoutePolicy),
		Origin:           cloudStatusOrigin(cfg.Cloud),
		UserBound:        cloudStatusUserBound(cfg.Cloud),
		CurrentAvailable: cfg.Cloud.Current != nil,
		Pending:          cfg.Cloud.Pending != nil,
	}
	if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
		summary.Status = "recovery_blocked"
		summary.VerificationError = cloudStatusErrorForProblem(hold)
		if cloudRecoveryHoldNeedsUnlock(
			cfg.Cloud.RecoveryHold,
		) {
			summary.NextCommand = cloudUnlockCommand()
		} else if cloudRecoveryHoldClearsAfterUnlock(
			cfg.Cloud.RecoveryHold,
		) {
			summary.NextCommand = cloudRemoveCommand()
		}
		if options.json {
			printCloudStatusJSON(summary)
			return 1
		}
		printHumanErr("%s", hold)
		if summary.CurrentAvailable {
			printHumanInfo(
				"The previously saved Cloud connection was not overwritten.",
			)
		}
		if cloudRecoveryHoldNeedsUnlock(cfg.Cloud.RecoveryHold) {
			printHumanInfo(
				"Verify native secure storage with: %s",
				cloudUnlockCommand(),
			)
		}
		printHumanInfo(
			"Verified cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return 1
	}
	if cfg.Cloud.cleanupPending() {
		detail := "remote Cloud revocation is complete; " +
			"finish the verified local cleanup before reconnecting"
		if cfg.Cloud.deviceCleanupPending() {
			detail = "Cloud device revocation is complete, but OAuth " +
				"authorization revocation remains; finish verified " +
				"Cloud cleanup before reconnecting"
		}
		problem := &cloudProblem{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSecurityStop,
			Detail:      detail,
		}
		summary.Status = "cleanup_pending"
		summary.VerificationError = cloudStatusErrorForProblem(problem)
		summary.NextCommand = cloudRemoveCommand()
		if options.json {
			printCloudStatusJSON(summary)
			return 1
		}
		printHumanErr("%s", problem)
		printHumanInfo(
			"Finish cleanup with: %s",
			summary.NextCommand,
		)
		return 1
	}
	if !cloudRemoteFeatureAvailable() {
		summary.VerificationError = cloudStatusErrorForProblem(
			cloudAdapterUnavailableProblem(),
		)
		if cfg.Cloud.Current != nil {
			summary.Status = "verification_failed"
		}
		if options.json {
			printCloudStatusJSON(summary)
			return 1
		}
		printHumanErr("%s", cloudAdapterUnavailableProblem())
		printHumanInfo(
			"Cloud transport is disabled in this build. Cleanup remains available with: %s",
			cloudRemoveCommand(),
		)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if cfg.Cloud.Current != nil {
		verifyErr := verifyCloudHealthAtSnapshot(
			ctx,
			paths,
			snapshot,
			verifyCloudDeviceHealthForCommand,
			nil,
		)
		if verifyErr != nil {
			if options.json {
				summary.Status = "verification_failed"
				problem := cloudProblemForCommandError(verifyErr)
				summary.VerificationError = cloudStatusErrorForProblem(problem)
				if problem.Remediation == cloudRemediationUnlockStorage {
					summary.NextCommand = cloudUnlockCommand()
				} else if cfg.Cloud.ready() &&
					cloudMutationRecoveryAvailable(problem) &&
					(problem.Remediation == cloudRemediationSignIn ||
						problem.Remediation == cloudRemediationPair) {
					summary.NextCommand = cloudReconnectCommand()
				} else if !cfg.Cloud.ready() &&
					cloudMutationRecoveryAvailable(problem) {
					summary.NextCommand = cloudResumeCommand(cfg)
				}
				printCloudStatusJSON(summary)
				return 1
			}
			problem := printCloudCommandProblem(verifyErr)
			if cfg.Cloud.ready() {
				printCloudReconnectGuidance(problem)
			}
			if !cfg.Cloud.ready() &&
				cloudMutationRecoveryAvailable(problem) {
				printHumanInfo(
					"A Cloud transaction is also waiting at %q. Resume after recovery with: %s",
					cfg.Cloud.State,
					cloudResumeCommand(cfg),
				)
			}
			return 1
		}
		summary.CurrentReady = true
	}
	if cfg.Cloud.ready() {
		summary.Status = "ready"
	} else {
		if summary.CurrentReady {
			summary.Status = "reconnect_pending"
		}
		if cloudRemoteFeatureAvailable() {
			summary.NextCommand = cloudResumeCommand(cfg)
		}
	}

	if options.json {
		printCloudStatusJSON(summary)
	} else if summary.Status == "ready" {
		printHumanInfo(
			"Home Assistant Cloud is ready for %q (%s; route %s).",
			summary.Server,
			summary.Origin,
			summary.RoutePolicy,
		)
	} else if summary.CurrentReady {
		printHumanInfo(
			"Current Home Assistant Cloud access is ready, but an update is waiting at %q.",
			summary.Lifecycle,
		)
		printCloudStatusResumeOrDisabled(summary.NextCommand)
	} else {
		printHumanInfo(
			"Home Assistant Cloud setup is waiting at %q.",
			summary.Lifecycle,
		)
		printCloudStatusResumeOrDisabled(summary.NextCommand)
	}
	if summary.Status != "ready" {
		return 1
	}
	return 0
}

func printCloudStatusResumeOrDisabled(nextCommand string) {
	if nextCommand != "" {
		printHumanInfo("Resume with: %s", nextCommand)
		return
	}
	printHumanInfo(
		"Cloud setup is paused because Cloud transport is disabled in this build. Cleanup remains available with: %s",
		cloudRemoveCommand(),
	)
}

func cloudStatusJSONRequested(args []string) bool {
	return scanCloudStatusArgs(args).jsonRequested
}

type cloudStatusRawIntent struct {
	jsonRequested bool
	serverSeen    bool
	serverValue   string
}

func scanCloudStatusArgs(args []string) cloudStatusRawIntent {
	var intent cloudStatusRawIntent
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			intent.jsonRequested = true
		case strings.HasPrefix(arg, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json="))
			if err == nil && value {
				intent.jsonRequested = true
			}
		case arg == "--server":
			value := ""
			if index+1 < len(args) &&
				!strings.HasPrefix(args[index+1], "--") {
				index++
				value = args[index]
			}
			intent.serverSeen = true
			intent.serverValue = value
		case strings.HasPrefix(arg, "--server="):
			value := strings.TrimPrefix(arg, "--server=")
			intent.serverSeen = true
			intent.serverValue = value
		}
	}
	return intent
}

func cloudStatusServerForReport(
	paths runtimePaths,
	options cloudCommandFlags,
	raw cloudStatusRawIntent,
) string {
	if raw.serverSeen {
		// Even invalid raw input stays attributed to itself. Falling through
		// could falsely report a configured profile that was never requested.
		return raw.serverValue
	}
	if options.server != "" {
		return options.server
	}
	if requested, _ := requestedServerSelection(); requested != "" {
		return requested
	}
	if configured := bestEffortCloudStatusDefaultServer(
		paths.ConfigFile,
	); configured != "" {
		return configured
	}
	if active := activeServerProfile(); active != "" {
		return active
	}
	return defaultServerProfileName
}

func bestEffortCloudStatusDefaultServer(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil || top == nil {
		return ""
	}
	var configured string
	if json.Unmarshal(top["default_server"], &configured) != nil {
		return ""
	}
	return strings.TrimSpace(configured)
}

func printCloudStatusJSON(summary cloudStatusSummary) {
	encoded, _ := json.Marshal(summary)
	fmt.Fprintln(os.Stdout, string(encoded))
}

func cloudStatusErrorForProblem(problem *cloudProblem) *cloudStatusError {
	if problem == nil {
		return nil
	}
	return &cloudStatusError{
		Code:        problem.Code,
		Remediation: problem.Remediation,
		Detail:      problem.Detail,
	}
}

func cloudStatusOrigin(metadata *cloudLifecycleMetadata) string {
	if metadata == nil {
		return ""
	}
	if metadata.Pending != nil {
		return metadata.Pending.Origin
	}
	if metadata.Current != nil {
		return metadata.Current.Origin
	}
	return ""
}

func cloudStatusUserBound(metadata *cloudLifecycleMetadata) bool {
	if metadata == nil {
		return false
	}
	if metadata.Pending != nil && metadata.Pending.HAUserID != "" {
		return true
	}
	return metadata.Current != nil && metadata.Current.HAUserID != ""
}
