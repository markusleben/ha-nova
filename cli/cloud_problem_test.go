package main

import (
	"strings"
	"testing"
)

func TestCloudProblemSecretPromptCanceledRequiresSecureStorageUnlock(t *testing.T) {
	problem := cloudProblemForError(
		newCloudError(
			CloudErrSecretPromptCanceled,
			"complete native secure-storage prompt",
			nil,
		),
	)
	if problem.Code != cloudProblemSecureStorage ||
		problem.Remediation != cloudRemediationUnlockStorage {
		t.Fatalf("cloud problem = %+v", problem)
	}
}

func TestCloudProblemOAuthOutcomeUnknownStopsForSessionReview(t *testing.T) {
	problem := cloudProblemForError(
		newCloudError(
			CloudErrOAuthOutcomeUnknown,
			"exchange OAuth authorization code",
			nil,
		),
	)
	if problem.Code != cloudProblemAuthorization ||
		problem.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("cloud problem = %+v", problem)
	}
}

func TestCloudProblemSecretOutcomeUnknownRequiresStateVerification(t *testing.T) {
	problem := cloudProblemForError(
		newCloudError(
			CloudErrSecretOutcomeUnknown,
			"write native secure storage",
			nil,
		),
	)
	if problem.Code != cloudProblemSecureStorage ||
		problem.Remediation != cloudRemediationVerifyState ||
		!strings.Contains(problem.Detail, "inspect the saved checkpoint") ||
		strings.Contains(problem.Detail, "resume") ||
		strings.Contains(problem.Detail, "retry") {
		t.Fatalf("cloud problem = %+v", problem)
	}
}

func TestCloudProblemRelayReinstallGivesExplicitFailClosedRecovery(t *testing.T) {
	problem := cloudProblemForError(
		newCloudError(
			CloudErrRelayInstance,
			"match Relay identity",
			nil,
		),
	)
	if problem.Code != cloudProblemIdentityMismatch ||
		problem.Remediation != cloudRemediationSecurityStop ||
		!strings.Contains(problem.Detail, "reinstalled") ||
		!strings.Contains(problem.Detail, "remove Cloud access") ||
		!strings.Contains(problem.Detail, "explicitly pair again") {
		t.Fatalf("Relay-reinstall problem = %+v", problem)
	}
}
