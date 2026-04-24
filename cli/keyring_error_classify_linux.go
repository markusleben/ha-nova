//go:build linux

package main

import "strings"

var inspectLinuxSecureStorageStateForClassification = inspectLinuxSecureStorageState

func classifyAmbiguousDesktopKeyringSetupError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "failed to unlock correct collection") {
		return nil
	}

	state, inspectErr := inspectLinuxSecureStorageStateForClassification()
	if inspectErr != nil {
		return nil
	}
	switch state.kind {
	case linuxSecureStorageStateNeedsInit:
		return desktopKeyringInitializationRequiredError(err.Error())
	case linuxSecureStorageStateLocked:
		return desktopKeyringLockedError(err.Error())
	default:
		return nil
	}
}
