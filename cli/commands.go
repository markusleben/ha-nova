package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var scheduleWindowsSelfDeleteForUninstall = scheduleWindowsSelfDelete
var waitForParentReleaseForUninstall = waitForParentRelease
var waitForParentReleaseForReplace = waitForParentRelease
var applyStagedBundleWithRollbackForReplace = applyStagedBundleWithRollback
var postUpdateSyncForReplace = postUpdateSyncWithResultUnlocked
var scheduleWindowsSelfDeleteForUpdate = scheduleWindowsSelfDelete
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

func ensureOptionalFileSnapshotCurrent(path string, snapshot []byte, existed bool) error {
	current, currentExists, err := readOptionalFile(path)
	if err != nil {
		return fmt.Errorf("read current configuration: %w", err)
	}
	if currentExists != existed || !bytes.Equal(current, snapshot) {
		return errors.New("server configuration changed during the operation; rerun the command")
	}
	return nil
}

func restoreOptionalFile(path string, data []byte, existed bool) error {
	if path == "" {
		return nil
	}
	if !existed {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}
