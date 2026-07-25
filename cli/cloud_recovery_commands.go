package main

import (
	"fmt"
	"os"
)

func selectedCloudCommandProfile() string {
	if requested, _ := requestedServerSelection(); requested != "" {
		if validateServerProfileName(requested) == nil {
			return requested
		}
		return ""
	}
	if active := activeServerProfile(); validateServerProfileName(active) == nil {
		return active
	}
	return ""
}

func cloudProfileCommand(action string) string {
	return cloudProfileCommandFor(action, selectedCloudCommandProfile())
}

func cloudProfileCommandFor(action, profile string) string {
	if profile == "" {
		return ""
	}
	return fmt.Sprintf(
		"ha-nova cloud %s --server %s",
		action,
		profile,
	)
}

func cloudRecoveryCommandProfile(paths runtimePaths) (string, error) {
	if requested, _ := requestedServerSelection(); requested != "" {
		if err := validateServerProfileName(requested); err != nil {
			return "", err
		}
		return requested, nil
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultServerProfileName, nil
		}
		return "", err
	}
	profile := doc.defaultServerName()
	if err := validateServerProfileName(profile); err != nil {
		return "", fmt.Errorf("invalid default_server: %w", err)
	}
	if !doc.hasProfile(profile) {
		return "", fmt.Errorf(
			"default_server %q does not exist in config.json",
			profile,
		)
	}
	return profile, nil
}

func cloudStatusCommand() string {
	return cloudProfileCommand("status")
}

func cloudUnlockCommand() string {
	return cloudProfileCommand("unlock")
}

func cloudReconnectCommand() string {
	return cloudProfileCommand("reconnect")
}

func cloudRemoveCommand() string {
	return cloudProfileCommand("remove")
}

func cloudSetupCommand() string {
	return cloudSetupCommandFor(selectedCloudCommandProfile())
}

func cloudSetupCommandFor(profile string) string {
	if profile == "" {
		return ""
	}
	return fmt.Sprintf(
		"ha-nova setup --server %s",
		profile,
	)
}

func cloudFreshAddCommand() string {
	return cloudProfileCommand("add")
}

func cloudResumeCommand(cfg runtimeConfig) string {
	return cloudResumeCommandFor(cfg, selectedCloudCommandProfile())
}

func cloudResumeCommandFor(cfg runtimeConfig, profile string) string {
	action := "add"
	if cfg.Cloud != nil &&
		cfg.Cloud.Current != nil &&
		cfg.Cloud.State != cloudStateCommitted &&
		cfg.Cloud.State != cloudStateRetiringPrevious {
		action = "reconnect"
	}
	return cloudProfileCommandFor(action, profile)
}

func cloudProblemNeedsReconnect(problem *cloudProblem) bool {
	return problem != nil &&
		(problem.Remediation == cloudRemediationSignIn ||
			problem.Remediation == cloudRemediationPair)
}

func cloudProblemBlocksMutationRecovery(problem *cloudProblem) bool {
	return problem != nil &&
		(problem.Remediation == cloudRemediationSecurityStop ||
			problem.Remediation == cloudRemediationVerifyState)
}

func cloudMutationRecoveryAvailable(problem *cloudProblem) bool {
	return cloudRemoteFeatureAvailable() &&
		!cloudProblemBlocksMutationRecovery(problem)
}

func printCloudReconnectGuidance(problem *cloudProblem) {
	if cloudMutationRecoveryAvailable(problem) &&
		cloudProblemNeedsReconnect(problem) {
		printHumanInfo("Reconnect with: %s", cloudReconnectCommand())
	}
}
