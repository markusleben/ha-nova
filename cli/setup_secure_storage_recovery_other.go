//go:build !linux

package main

func detectPlatformSecureStorageRecoverySupport() (bool, error) {
	return false, nil
}

func inferPlatformSecureStorageRecoveryAction(err error) (platformSecureStorageRecoveryAction, error) {
	_ = err
	return "", nil
}

func runPlatformSecureStorageRecovery(action platformSecureStorageRecoveryAction, secret []byte) error {
	_ = action
	_ = secret
	return nil
}
