package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type clientStatus struct {
	ID              string
	Label           string
	SupportedOnOS   bool
	RuntimeDetected bool
	Configured      bool
	Attached        bool
	Ready           bool
	Reason          string
}

var clientRuntimeDetectedForStatus = clientRuntimeDetected
var clientAttachmentPresentForStatus = clientAttachmentPresent

func evaluateClientStatus(paths runtimePaths, state installState, client clientRegistryEntry) clientStatus {
	status := clientStatus{
		ID:            client.ID,
		Label:         client.Label,
		SupportedOnOS: clientSupportedOnCurrentOS(client),
	}
	claudeSnapshot := claudeInstallSnapshot{}
	if client.ID == "claude" {
		claudeSnapshot = inspectClaudeInstallSnapshot(paths, state)
	}
	status.Attached = clientAttachmentPresentForStatus(paths, state, client.ID)
	status.Configured = containsClient(state.InstalledClients, client.ID) || status.Attached
	if client.ID == "claude" && (claudeSnapshot.MarketplaceFound || claudeSnapshot.PluginFound) {
		status.Configured = true
	}
	if client.ID == "hermes" && hermesLegacyBundlePresent(paths.Home) {
		status.Configured = true
	}
	status.RuntimeDetected = clientRuntimeDetectedForStatus(client.ID)
	status.Ready = status.SupportedOnOS && status.RuntimeDetected && status.Attached
	status.Reason = clientStatusReason(client.ID, status)
	return status
}

func clientRuntimeDetected(client string) bool {
	command := clientRuntimeCommand(client)
	if command == "" {
		return false
	}
	_, err := exec.LookPath(command)
	return err == nil
}

func clientRuntimeCommand(client string) string {
	switch client {
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	case "gemini":
		return "gemini"
	case "hermes":
		return "hermes"
	default:
		return ""
	}
}

func clientAttachmentPresent(paths runtimePaths, state installState, client string) bool {
	switch client {
	case "codex":
		return fileExists(filepath.Join(paths.Home, ".agents", "skills", "ha-nova", "ha-nova", "SKILL.md"))
	case "opencode":
		return fileExists(filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova", "ha-nova", "SKILL.md"))
	case "gemini":
		return fileExists(filepath.Join(paths.Home, ".gemini", "skills", "ha-nova", "SKILL.md")) &&
			fileExists(filepath.Join(paths.Home, ".gemini", "skills", "ha-nova-review", "SKILL.md"))
	case "hermes":
		return hermesBundlePresent(paths.Home)
	case "claude":
		return inspectClaudeInstallSnapshot(paths, state).Attached
	default:
		return false
	}
}

func configuredClientStatuses(paths runtimePaths, state installState) ([]clientStatus, error) {
	statuses := []clientStatus{}
	clients, err := loadClientRegistry(paths)
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		status := evaluateClientStatus(paths, state, client)
		if status.Configured {
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}

func clientStatusReason(client string, status clientStatus) string {
	if !status.SupportedOnOS {
		return fmt.Sprintf("not supported on %s", bundlePlatformOS())
	}
	if status.RuntimeDetected {
		return ""
	}
	switch client {
	case "claude":
		return "install Claude Code first"
	case "codex":
		return "install Codex CLI first"
	case "opencode":
		if runtime.GOOS == "windows" {
			return "install OpenCode natively first, or run HA NOVA setup inside WSL"
		}
		return "install OpenCode first"
	case "gemini":
		return "install Gemini CLI first"
	case "hermes":
		return "install Hermes Agent first"
	default:
		return "install this client first"
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
