package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

var scheduleWindowsSelfDeleteForUninstall = scheduleWindowsSelfDelete
var waitForParentReleaseForReplace = waitForParentRelease
var applyStagedBundleWithRollbackForReplace = applyStagedBundleWithRollback
var postUpdateSyncForReplace = postUpdateSync
var runWingetUpgradeForUpdate = runWingetUpgrade
var launchWindowsWingetUpgradeForUpdate = launchWindowsWingetUpgrade
var runInstalledSyncForWingetUpdate = runUpdatedRuntimeClientSync
var scheduleWindowsSelfDeleteForUpdate = scheduleWindowsSelfDelete
var waitForParentReleaseForWingetUpdate = waitForParentRelease
var execLookPathForLifecycle = exec.LookPath
var copyToClipboardForSetup = copyToClipboard
var openBrowserForSetup = openBrowser
var writeRelayAuthTokenForSetup = writeRelayAuthToken
var saveConfigForSetup = saveConfig
var saveStateForSetup = saveState
var removeClaudeProjectMemoryForUninstall = removeClaudeProjectMemoryWithReport

func shouldDeleteRelayAuthTokenOnUninstall() bool {
	return true
}

func printPostUpdateSyncFailure(err error) {
	printHumanErr("runtime updated, but post-update client sync failed: %s", err)
	printHumanInfo("Run 'ha-nova setup <client>' after fixing the client-side issue.")
}

type installRootReplacement struct {
	backupRoot string
	hadOld     bool
}

func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func restoreOptionalFile(path string, data []byte, existed bool) {
	if path == "" {
		return
	}
	if !existed {
		_ = os.Remove(path)
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}
