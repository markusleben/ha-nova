package main

import (
	"errors"
	"os"
)

func cloudStatusHandledInvalidInstallIdentity(
	paths runtimePaths,
	options cloudCommandFlags,
	loadErr error,
) bool {
	if !errors.Is(loadErr, errInvalidClientInstallID) {
		return false
	}
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		return false
	}
	cfg := snapshot.Config
	problem := invalidCloudInstallIdentityProblem(loadErr)
	summary := cloudStatusSummary{
		Status:            "recovery_blocked",
		Server:            snapshot.ProfileName,
		RoutePolicy:       effectiveRoutePolicy(cfg.RoutePolicy),
		VerificationError: cloudStatusErrorForProblem(problem),
	}
	if cfg.Cloud != nil {
		summary.Lifecycle = cfg.Cloud.State
		summary.Origin = cloudStatusOrigin(cfg.Cloud)
		summary.UserBound = cloudStatusUserBound(cfg.Cloud)
		summary.CurrentAvailable = cfg.Cloud.Current != nil
		summary.Pending = cfg.Cloud.Pending != nil
		summary.NextCommand = cloudProfileCommandFor(
			"remove",
			snapshot.ProfileName,
		)
	} else if command, needsRecovery := invalidInstallIdentityRecoveryCommand(
		paths,
		snapshot.ProfileName,
	); needsRecovery {
		summary.NextCommand = command
	}
	if options.json {
		printCloudStatusJSON(summary)
		return true
	}
	printHumanErr("%s", problem)
	if cfg.Cloud != nil {
		renderCloudCheckpointActionsForProfile(
			os.Stdout,
			cfg,
			false,
			snapshot.ProfileName,
		)
	} else if summary.NextCommand != "" {
		printHumanInfo("Continue recovery with: %s", summary.NextCommand)
	}
	return true
}

func invalidInstallIdentityRecoveryCommand(
	paths runtimePaths,
	selectedProfile string,
) (string, bool) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil ||
		validateClientInstallID(doc.meta.ClientInstallID) == nil {
		return "", false
	}
	cleanupProfile, cloudRemains, err :=
		remainingCloudCleanupProfile(doc)
	switch {
	case err != nil:
		return cloudProfileCommandFor(
			"status",
			selectedProfile,
		), true
	case cloudRemains && cleanupProfile != "":
		return cloudProfileCommandFor("remove", cleanupProfile), true
	case cloudRemains:
		return cloudProfileCommandFor("status", selectedProfile), true
	default:
		return cloudSetupCommandFor(selectedProfile), true
	}
}
