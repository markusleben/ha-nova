package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudePluginOlderThan: installed_plugins.json itself holds the usable HA
// NOVA install, and its record is older than version ("": nothing to
// compare, as on a dev install). An install only the claude CLI vouched for
// is never second-guessed by a stale file record.
func claudePluginOlderThan(home, version string) bool {
	if version == "" {
		return false
	}
	found, usable, unreadable := readClaudePluginInstallState(home)
	return found && usable && !unreadable && claudeHANovaAt(claudeConfigRoot(home), version) == claudeHANovaOlder
}

// verifyClaudePluginInstalled: after a sync, Claude Code loads HA NOVA from
// desiredSource, at this release. A sync Claude Code reported as done that
// kept an older record gets one remove and a fresh install; still older is
// an error, never a success.
func verifyClaudePluginInstalled(paths runtimePaths, desiredSource string) error {
	if err := verifyClaudePluginPresent(paths.Home, desiredSource); err != nil {
		return err
	}
	version := ""
	if expected, release, err := expectedClaudeMarketplaceSource(paths, loadStateOrDefault(paths)); err == nil && filepath.Clean(expected) == filepath.Clean(desiredSource) {
		version = release
	}
	if !claudePluginOlderThan(paths.Home, version) {
		return nil
	}
	if err := resetClaudeLocalPluginState(paths.Home); err != nil {
		return err
	}
	if output, err := claudeCommand("plugin", "install", "ha-nova@ha-nova").CombinedOutput(); err != nil {
		return fmt.Errorf("claude plugin command failed: plugin install ha-nova@ha-nova (%s)", strings.TrimSpace(string(output)))
	}
	if err := verifyClaudePluginPresent(paths.Home, desiredSource); err != nil {
		return err
	}
	if claudePluginOlderThan(paths.Home, version) {
		return fmt.Errorf("%w, even after a fresh install of %s", errClaudeKeptOlderPlugin, version)
	}
	return nil
}

var errClaudeKeptOlderPlugin = errors.New("Claude Code kept an older HA NOVA")

func verifyClaudePluginPresent(home, desiredSource string) error {
	pluginPresent, usableInstallPath := readClaudePluginInstallSnapshot(home)
	if !pluginPresent {
		return fmt.Errorf("Claude plugin ha-nova@ha-nova not found after sync")
	}
	if !usableInstallPath {
		return fmt.Errorf("Claude plugin ha-nova@ha-nova installPath missing after sync")
	}
	currentSource, hasCurrentSource, err := readClaudeMarketplaceSource(home)
	if err != nil {
		return err
	}
	if !hasCurrentSource {
		return fmt.Errorf("Claude marketplace ha-nova not found after sync")
	}
	if !sameClaudeMarketplaceSource(currentSource, desiredSource) {
		return fmt.Errorf("Claude marketplace ha-nova source mismatch after sync")
	}
	return nil
}

// A Claude Code that keeps an older HA NOVA even after a fresh install
// would make the session-start auto-repair reinstall it at every start.
// The release it failed for is noted; auto-repair skips that release (a
// new release, setup, or doctor still try), and a success clears the note.
func claudeRepairFailedPath(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "claude-repair-failed")
}

func claudeRepairFailedFor(paths runtimePaths, version string) bool {
	data, err := os.ReadFile(claudeRepairFailedPath(paths))
	return err == nil && version != "" && strings.TrimSpace(string(data)) == version
}

// claudeRepairStuck: Claude Code still holds an older HA NOVA, and a repair
// to this release already failed that way. Automatic repairs skip it.
func claudeRepairStuck(paths runtimePaths, state installState) bool {
	snapshot := inspectClaudeInstallSnapshot(paths, state)
	return snapshot.MismatchReason == claudePluginOutdated && claudeRepairFailedFor(paths, snapshot.ExpectedVersion)
}

// noteClaudeRepairAfter notes how a Claude Code install (err) ended, and
// returns err with any failure to note it.
func noteClaudeRepairAfter(paths runtimePaths, state installState, err error) error {
	_, version, _ := expectedClaudeMarketplaceSource(paths, state)
	return errors.Join(err, noteClaudeRepair(paths, version, errors.Is(err, errClaudeKeptOlderPlugin)))
}

// noteClaudeRepair records how a Claude Code repair for version ended.
func noteClaudeRepair(paths runtimePaths, version string, failed bool) error {
	if failed {
		if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
			return err
		}
		return writeFileAtomic(claudeRepairFailedPath(paths), []byte(version+"\n"), 0o600)
	}
	if err := os.Remove(claudeRepairFailedPath(paths)); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}
