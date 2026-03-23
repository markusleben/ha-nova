package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	windowsUninstallStatusSchemaVersion = 1
	windowsUninstallStatusOperation     = "uninstall"
	windowsUninstallStatusRunning       = "running"
	windowsUninstallStatusFailed        = "failed"
	windowsUninstallStatusSuccess       = "success"
	windowsUninstallStatusTimeout       = 10 * time.Minute
)

type windowsUninstallStatus struct {
	SchemaVersion  int       `json:"schema_version"`
	Operation      string    `json:"operation"`
	Status         string    `json:"status"`
	Mode           string    `json:"mode"`
	InstallSource  string    `json:"install_source"`
	HelperPID      int       `json:"helper_pid,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	LastUpdatedAt  time.Time `json:"last_updated_at,omitempty"`
	ErrorSummary   string    `json:"error_summary,omitempty"`
	ErrorDetails   string    `json:"error_details,omitempty"`
	FailingStep    string    `json:"failing_step,omitempty"`
	RemainingPaths []string  `json:"remaining_paths,omitempty"`
	InstallRoot    string    `json:"install_root,omitempty"`
}

type windowsUninstallStatusKind string

const (
	windowsUninstallStatusKindNone        windowsUninstallStatusKind = "none"
	windowsUninstallStatusKindRunning     windowsUninstallStatusKind = "running"
	windowsUninstallStatusKindInterrupted windowsUninstallStatusKind = "interrupted"
	windowsUninstallStatusKindFailed      windowsUninstallStatusKind = "failed"
	windowsUninstallStatusKindCorrupt     windowsUninstallStatusKind = "corrupt"
)

type windowsUninstallStatusInspection struct {
	Kind            windowsUninstallStatusKind
	Status          windowsUninstallStatus
	Summary         string
	RecoveryCommand string
}

var windowsUninstallStatusChecksEnabled = func() bool {
	return runtime.GOOS == "windows"
}

var windowsUninstallStatusNow = time.Now

var windowsUninstallStatusProcessAlive = func(pid int) bool {
	if pid <= 0 || runtime.GOOS != "windows" {
		return false
	}
	cmd := buildWindowsHiddenPowerShellCommand(fmt.Sprintf(`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { exit 0 }; exit 1`, pid))
	return cmd.Run() == nil
}

func inspectWindowsUninstallStatus(paths runtimePaths) windowsUninstallStatusInspection {
	if !windowsUninstallStatusChecksEnabled() || strings.TrimSpace(paths.UninstallStatusFile) == "" {
		return windowsUninstallStatusInspection{Kind: windowsUninstallStatusKindNone}
	}

	status, err := loadWindowsUninstallStatus(paths)
	if err != nil {
		if isNotExist(err) {
			return windowsUninstallStatusInspection{Kind: windowsUninstallStatusKindNone}
		}
		return windowsUninstallStatusInspection{
			Kind:            windowsUninstallStatusKindCorrupt,
			Summary:         "A previous background HA NOVA uninstall left an unreadable recovery marker.",
			RecoveryCommand: windowsUninstallRecoveryCommand(uninstallModeStandard),
		}
	}

	switch status.Status {
	case windowsUninstallStatusSuccess:
		_ = removeWindowsUninstallStatus(paths)
		return windowsUninstallStatusInspection{Kind: windowsUninstallStatusKindNone}
	case windowsUninstallStatusRunning:
		if windowsUninstallStatusStillActive(status) {
			return windowsUninstallStatusInspection{
				Kind:            windowsUninstallStatusKindRunning,
				Status:          status,
				Summary:         "A background HA NOVA uninstall is still running on Windows.",
				RecoveryCommand: windowsUninstallRecoveryCommand(normalizeUninstallMode(status.Mode)),
			}
		}
		return windowsUninstallStatusInspection{
			Kind:            windowsUninstallStatusKindInterrupted,
			Status:          status,
			Summary:         "A previous background HA NOVA uninstall was interrupted before it finished.",
			RecoveryCommand: windowsUninstallRecoveryCommand(normalizeUninstallMode(status.Mode)),
		}
	case windowsUninstallStatusFailed:
		summary := strings.TrimSpace(status.ErrorSummary)
		if summary == "" {
			summary = "A previous background HA NOVA uninstall did not finish cleanly."
		}
		return windowsUninstallStatusInspection{
			Kind:            windowsUninstallStatusKindFailed,
			Status:          status,
			Summary:         summary,
			RecoveryCommand: windowsUninstallRecoveryCommand(normalizeUninstallMode(status.Mode)),
		}
	default:
		return windowsUninstallStatusInspection{
			Kind:            windowsUninstallStatusKindCorrupt,
			Status:          status,
			Summary:         "A previous background HA NOVA uninstall left an unreadable recovery marker.",
			RecoveryCommand: windowsUninstallRecoveryCommand(normalizeUninstallMode(status.Mode)),
		}
	}
}

func loadWindowsUninstallStatus(paths runtimePaths) (windowsUninstallStatus, error) {
	data, err := os.ReadFile(paths.UninstallStatusFile)
	if err != nil {
		return windowsUninstallStatus{}, err
	}
	var status windowsUninstallStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return windowsUninstallStatus{}, err
	}
	status.SchemaVersion = windowsUninstallStatusSchemaVersion
	status.Operation = windowsUninstallStatusOperation
	status.Mode = string(normalizeUninstallMode(status.Mode))
	status.InstallSource = normalizeInstallSource(status.InstallSource)
	return status, nil
}

func beginWindowsUninstallStatus(paths runtimePaths, mode uninstallMode, installSource string) (*windowsUninstallStatus, error) {
	status := &windowsUninstallStatus{
		SchemaVersion: windowsUninstallStatusSchemaVersion,
		Operation:     windowsUninstallStatusOperation,
		Status:        windowsUninstallStatusRunning,
		Mode:          string(mode),
		InstallSource: normalizeInstallSource(installSource),
		HelperPID:     os.Getpid(),
		StartedAt:     windowsUninstallStatusNow().UTC(),
		LastUpdatedAt: windowsUninstallStatusNow().UTC(),
		InstallRoot:   paths.InstallRoot,
	}
	if err := writeWindowsUninstallStatus(paths, *status); err != nil {
		return nil, err
	}
	return status, nil
}

func updateWindowsUninstallStatusProgress(paths runtimePaths, status *windowsUninstallStatus) error {
	if status == nil {
		return nil
	}
	status.LastUpdatedAt = windowsUninstallStatusNow().UTC()
	return writeWindowsUninstallStatus(paths, *status)
}

func failWindowsUninstallStatus(paths runtimePaths, status *windowsUninstallStatus, step string, err error) error {
	if status == nil {
		return err
	}
	status.Status = windowsUninstallStatusFailed
	status.LastUpdatedAt = windowsUninstallStatusNow().UTC()
	status.FailingStep = strings.TrimSpace(step)
	status.ErrorDetails = strings.TrimSpace(err.Error())
	status.ErrorSummary = windowsUninstallErrorSummary(step, err)
	status.RemainingPaths = collectWindowsUninstallRemainingPaths(paths, normalizeUninstallMode(status.Mode), status.InstallSource)
	if writeErr := writeWindowsUninstallStatus(paths, *status); writeErr != nil {
		return fmt.Errorf("%w (and could not persist uninstall recovery state: %v)", err, writeErr)
	}
	return err
}

func finishWindowsUninstallStatus(paths runtimePaths, status *windowsUninstallStatus) error {
	if status == nil {
		return nil
	}
	status.Status = windowsUninstallStatusSuccess
	status.LastUpdatedAt = windowsUninstallStatusNow().UTC()
	status.ErrorSummary = ""
	status.ErrorDetails = ""
	status.FailingStep = ""
	status.RemainingPaths = nil
	if err := writeWindowsUninstallStatus(paths, *status); err != nil {
		_ = removeWindowsUninstallStatus(paths)
		return err
	}
	return removeWindowsUninstallStatus(paths)
}

func writeWindowsUninstallStatus(paths runtimePaths, status windowsUninstallStatus) error {
	status.SchemaVersion = windowsUninstallStatusSchemaVersion
	status.Operation = windowsUninstallStatusOperation
	status.Mode = string(normalizeUninstallMode(status.Mode))
	status.InstallSource = normalizeInstallSource(status.InstallSource)
	if strings.TrimSpace(status.InstallRoot) == "" {
		status.InstallRoot = paths.InstallRoot
	}
	if status.StartedAt.IsZero() {
		status.StartedAt = windowsUninstallStatusNow().UTC()
	}
	if status.LastUpdatedAt.IsZero() {
		status.LastUpdatedAt = status.StartedAt
	}
	status.RemainingPaths = normalizePathList(status.RemainingPaths)
	return writeJSONFile(paths.UninstallStatusFile, status, 0o600)
}

func removeWindowsUninstallStatus(paths runtimePaths) error {
	if strings.TrimSpace(paths.UninstallStatusFile) == "" {
		return nil
	}
	if err := os.Remove(paths.UninstallStatusFile); err != nil && !isNotExist(err) {
		return err
	}
	if err := removeDirIfEmptyWithReport(filepath.Dir(paths.UninstallStatusFile), nil); err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

func windowsUninstallRecoveryCommand(mode uninstallMode) string {
	if normalizeUninstallMode(string(mode)) == uninstallModePurge {
		return "ha-nova uninstall --yes --purge"
	}
	return "ha-nova uninstall --yes"
}

func normalizeUninstallMode(value string) uninstallMode {
	if strings.EqualFold(strings.TrimSpace(value), string(uninstallModePurge)) {
		return uninstallModePurge
	}
	return uninstallModeStandard
}

func windowsUninstallStatusStillActive(status windowsUninstallStatus) bool {
	if status.HelperPID <= 0 || !windowsUninstallStatusProcessAlive(status.HelperPID) {
		return false
	}
	updated := status.LastUpdatedAt
	if updated.IsZero() {
		updated = status.StartedAt
	}
	if updated.IsZero() {
		return false
	}
	return windowsUninstallStatusNow().UTC().Sub(updated.UTC()) < windowsUninstallStatusTimeout
}

func windowsUninstallErrorSummary(step string, err error) string {
	step = strings.TrimSpace(step)
	switch step {
	case "client_integrations":
		return "HA NOVA uninstall could not remove all client integrations."
	case "path_cleanup":
		return "HA NOVA uninstall could not finish PATH cleanup."
	case "config_cleanup":
		return "HA NOVA uninstall could not remove managed config artifacts."
	case "cache_cleanup":
		return "HA NOVA uninstall could not remove managed cache artifacts."
	case "token_cleanup":
		return "HA NOVA uninstall could not remove the stored relay token."
	case "bundle_runtime_cleanup":
		return "HA NOVA uninstall could not remove the Windows bundle runtime."
	case "winget_runtime_cleanup":
		return "HA NOVA uninstall could not remove the Windows winget package."
	default:
		if err != nil {
			return strings.TrimSpace(err.Error())
		}
		return "HA NOVA uninstall did not finish cleanly."
	}
}

func collectWindowsUninstallRemainingPaths(paths runtimePaths, mode uninstallMode, installSource string) []string {
	candidates := []string{
		paths.InstallRoot,
		paths.PublicBinary,
		paths.StateFile,
		paths.UpdateCacheFile,
		paths.CacheDir,
		filepath.Join(paths.ConfigDir, "claude-marketplace"),
		paths.ConfigDir,
	}
	if mode == uninstallModePurge {
		candidates = append(candidates, paths.ConfigFile)
	}
	if normalizeInstallSource(installSource) == installSourceWinget {
		candidates = append(candidates, windowsWingetLinkPath(paths.Home))
	}
	remaining := []string{}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			remaining = append(remaining, candidate)
		}
	}
	return normalizePathList(remaining)
}

func normalizePathList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
