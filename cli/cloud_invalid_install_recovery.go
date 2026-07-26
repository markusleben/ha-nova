package main

import (
	"errors"
	"os"
)

var loadConfigDocumentForInvalidInstallRecovery = loadConfigDocument

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
	} else if command, needsRecovery, inspectionErr :=
		invalidInstallIdentityRecoveryCommand(
			paths,
			snapshot.ProfileName,
		); needsRecovery {
		summary.NextCommand = command
		if inspectionErr != nil {
			summary.VerificationError.Detail +=
				"; Cloud cleanup inventory requires manual config review: " +
					inspectionErr.Error()
		}
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
	} else {
		printHumanErr(
			"Cloud cleanup inventory cannot be resolved safely. Preserve config.json and have its server profiles reviewed manually before continuing.",
		)
	}
	return true
}

func invalidInstallIdentityRecoveryCommand(
	paths runtimePaths,
	selectedProfile string,
) (string, bool, error) {
	doc, err := loadConfigDocumentForInvalidInstallRecovery(
		paths.ConfigFile,
	)
	if err != nil {
		return "", true, err
	}
	if validateClientInstallID(doc.meta.ClientInstallID) == nil {
		return "", false, nil
	}
	cleanupProfile, cloudRemains, err :=
		remainingCloudCleanupProfile(doc)
	switch {
	case err != nil:
		return "", true, err
	case cloudRemains && cleanupProfile != "":
		return cloudProfileCommandFor("remove", cleanupProfile), true, nil
	case cloudRemains:
		return "", true, errors.New(
			"unscoped top-level Cloud state exists beside server profiles",
		)
	default:
		return cloudSetupCommandFor(selectedProfile), true, nil
	}
}
