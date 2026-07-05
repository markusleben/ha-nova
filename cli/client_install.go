package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func installClients(paths runtimePaths, state *installState, clients []string) error {
	sourceRoot := resolveSourceRoot(paths)
	for _, client := range normalizeClients(clients) {
		mode, err := installClient(paths, sourceRoot, client)
		if err != nil {
			return err
		}
		if state != nil {
			if state.ClientInstallModes == nil {
				state.ClientInstallModes = map[string]string{}
			}
			state.ClientInstallModes[client] = mode
			mergeStateClients(state, []string{client})
		}
	}
	return nil
}

func installClient(paths runtimePaths, sourceRoot, client string) (string, error) {
	requestedClient := client
	client = canonicalClientID(client)
	entry, ok, err := findRegistryClient(paths, client)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("unsupported client: %s", requestedClient)
	}
	status := evaluateClientStatus(paths, installState{}, entry)
	if !status.SupportedOnOS {
		return "", fmt.Errorf("%s is not supported on %s", entry.Label, runtime.GOOS)
	}
	if !status.RuntimeDetected {
		return "", fmt.Errorf("%s is not available yet: %s", entry.Label, status.Reason)
	}

	switch entry.AdapterKind {
	case "skill_tree":
		switch client {
		case "codex":
			return installTreeClient(filepath.Join(paths.Home, ".agents", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
		case "opencode":
			return installTreeClient(filepath.Join(paths.Home, ".config", "opencode", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
		case "hermes":
			return "copy", installHermesClient(paths.Home, sourceRoot)
		default:
			return "", fmt.Errorf("unsupported skill-tree client: %s", client)
		}
	case "skill_flat":
		if client != "antigravity" {
			return "", fmt.Errorf("unsupported skill-flat client: %s", client)
		}
		return "copy", installAntigravityClient(paths.Home, sourceRoot)
	case "plugin_marketplace":
		if client != "claude" {
			return "", fmt.Errorf("unsupported plugin-marketplace client: %s", client)
		}
		return "plugin", installClaudePlugin(paths, sourceRoot)
	default:
		return "", fmt.Errorf("unsupported adapter kind: %s", entry.AdapterKind)
	}
}

func preferSymlinkForCurrentOS() bool {
	return runtime.GOOS != "windows"
}

func installTreeClient(parentDir, sourceDir string, preferSymlink bool) (string, error) {
	targetDir := filepath.Join(parentDir, "ha-nova")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return "", err
	}
	_ = os.RemoveAll(targetDir)

	if preferSymlink {
		if err := os.Symlink(sourceDir, targetDir); err == nil {
			return "symlink", nil
		}
	}

	if err := copyDir(sourceDir, targetDir); err != nil {
		return "", err
	}
	return "copy", nil
}
