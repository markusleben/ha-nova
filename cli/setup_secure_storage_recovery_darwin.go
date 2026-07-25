//go:build darwin

package main

import (
	"os"
	"syscall"
)

var darwinConsoleUserSessionAvailable = func() bool {
	info, err := os.Stat("/dev/console")
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Uid != 0
}

func platformNativeSecretPromptContextAvailable() bool {
	return nativeSecretPromptBaseContextAvailable() &&
		darwinConsoleUserSessionAvailable()
}

func detectPlatformSecureStorageRecoverySupport() (bool, error) {
	return false, nil
}

func inferPlatformSecureStorageRecoveryAction(
	err error,
) (platformSecureStorageRecoveryAction, error) {
	_ = err
	return "", nil
}

func runPlatformSecureStorageRecovery(
	action platformSecureStorageRecoveryAction,
) error {
	_ = action
	return nil
}
