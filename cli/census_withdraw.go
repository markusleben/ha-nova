package main

import (
	"fmt"
	"time"
)

type censusWithdrawalResult struct {
	Attempted bool
	Confirmed bool
	Err       error
}

func disableAndWithdrawCensus(paths runtimePaths, markAnswer bool) (censusWithdrawalResult, error) {
	release, ok := acquireCensusLock(paths)
	if !ok {
		return censusWithdrawalResult{}, fmt.Errorf("census state is locked by another process")
	}
	defer release()
	state, writable := readCensusState(paths)
	if !writable {
		return censusWithdrawalResult{}, fmt.Errorf("census state is not writable by this client version")
	}
	return disableAndWithdrawCensusLocked(paths, &state, markAnswer)
}

// disableAndWithdrawCensusLocked is shared with uninstall, which already owns
// the Census lock. Consent is persisted before the bounded network request, so
// withdrawal failure can never leave reporting enabled.
func disableAndWithdrawCensusLocked(
	paths runtimePaths,
	state *censusState,
	markAnswer bool,
) (censusWithdrawalResult, error) {
	state.Enabled = false
	state.PendingChoiceID = ""
	if markAnswer {
		state.Answer = "no"
		state.ConsentVersion = censusConsentVersion
		if state.AskedAt == "" {
			state.AskedAt = censusNow().UTC().Format(time.RFC3339)
			state.AskedVia = "command"
		}
	}
	// A server-side record can exist only after a locally recorded attempt.
	// Avoid an unnecessary network request for a freshly declined prompt.
	state.WithdrawalPending = state.LastAttemptAt != "" &&
		censusInstallationIDPattern.MatchString(state.InstallationID)
	if err := saveCensusState(paths, *state); err != nil {
		return censusWithdrawalResult{}, err
	}
	if !state.WithdrawalPending ||
		!censusEndpointConfigured() ||
		censusOptedOutByEnv() ||
		BuildChannel == "dev" ||
		localVersion(paths) == "dev" {
		return censusWithdrawalResult{}, nil
	}

	result := censusWithdrawalResult{Attempted: true}
	if err := postCensusWithdraw(state.InstallationID); err != nil {
		result.Err = err
		return result, nil
	}
	state.WithdrawalPending = false
	state.InstallationID = ""
	state.LastAttemptAt = ""
	if err := saveCensusState(paths, *state); err != nil {
		result.Err = fmt.Errorf("withdrawal confirmed but local state update failed: %w", err)
		return result, nil
	}
	result.Confirmed = true
	return result, nil
}
