//go:build !linux && !darwin

package main

func platformNativeSecretPromptContextAvailable() bool {
	return nativeSecretPromptBaseContextAvailable()
}

func detectPlatformSecureStorageRecoverySupport() (bool, error) {
	return false, nil
}

func inferPlatformSecureStorageRecoveryAction(err error) (platformSecureStorageRecoveryAction, error) {
	_ = err
	return "", nil
}

func runPlatformSecureStorageRecovery(action platformSecureStorageRecoveryAction) error {
	_ = action
	return nil
}
