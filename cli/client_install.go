package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func installClients(paths runtimePaths, state *installState, clients []string) error {
	return withClientMutationLock(paths, func() error {
		return installClientsWithPolicy(paths, state, clients, false)
	})
}

func installClientsAndSaveState(
	paths runtimePaths,
	state *installState,
	clients []string,
	save func(runtimePaths, installState) error,
	lifecycleMarker ...[]byte,
) error {
	return withClientMutationLock(paths, func() error {
		return installClientsAndSaveStateUnlocked(paths, state, clients, save, lifecycleMarker...)
	})
}

func installClientsAndSaveStateUnlocked(
	paths runtimePaths,
	state *installState,
	clients []string,
	save func(runtimePaths, installState) error,
	lifecycleMarker ...[]byte,
) error {
	if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
		return err
	}
	latest, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return err
	}
	*state = latest
	if err := installClientsWithPolicy(paths, state, clients, false); err != nil {
		return err
	}
	if allTrackedClientsSynced(state.InstalledClients, clients) {
		state.ClientsVerifiedVersion = localVersion(paths)
	}
	return save(paths, *state)
}

func ensureSetupLifecycleCurrent(paths runtimePaths, lifecycleMarker ...[]byte) error {
	if len(lifecycleMarker) > 0 {
		current, err := readInstallLifecycleGeneration(paths)
		if err != nil {
			return fmt.Errorf("cannot verify setup lifecycle: %w", err)
		}
		if !bytes.Equal(current, lifecycleMarker[0]) {
			return fmt.Errorf("setup was superseded by an uninstall; rerun setup")
		}
	}
	if len(lifecycleMarker) > 1 {
		current, err := readCensusLifecycleMarker(paths)
		if err != nil {
			return fmt.Errorf("cannot verify setup lifecycle: %w", err)
		}
		if !bytes.Equal(current, lifecycleMarker[1]) {
			return fmt.Errorf("setup was superseded by an uninstall; rerun setup")
		}
	}
	if len(lifecycleMarker) > 2 {
		current, err := readSetupConfigSnapshot(paths)
		if err != nil {
			return fmt.Errorf("cannot verify setup server configuration: %w", err)
		}
		if !bytes.Equal(current, lifecycleMarker[2]) {
			return fmt.Errorf("server configuration changed during setup; rerun setup")
		}
	}
	return nil
}

func readSetupConfigSnapshot(paths runtimePaths) ([]byte, error) {
	data, existed, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	prefix := byte(0)
	if existed {
		prefix = 1
	}
	return append([]byte{prefix}, data...), nil
}

func refreshSetupConfigSnapshot(paths runtimePaths, lifecycleMarker [][]byte) error {
	if len(lifecycleMarker) <= 2 {
		return nil
	}
	current, err := readSetupConfigSnapshot(paths)
	if err != nil {
		return err
	}
	lifecycleMarker[2] = current
	return nil
}

func completeSetupLifecycle(paths runtimePaths, lifecycleMarker ...[]byte) error {
	if len(lifecycleMarker) == 0 {
		return nil
	}
	return withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		return completeSetupLifecycleUnlocked(paths, lifecycleMarker...)
	})
}

func completeSetupLifecycleUnlocked(paths runtimePaths, lifecycleMarker ...[]byte) error {
	if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
		return err
	}
	release, acquired := acquireCensusLock(paths)
	if !acquired {
		return fmt.Errorf("cannot acquire census lifecycle lock")
	}
	defer release()
	// Uninstall owns the same census lock while rotating both lifecycle
	// markers. Recheck after acquiring it so setup cannot finalize across an
	// uninstall that won the lock after the optimistic check above.
	if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
		return err
	}
	var censusMarker []byte
	if len(lifecycleMarker) > 1 {
		censusMarker = lifecycleMarker[1]
	}
	reactivated, err := reactivateCensusAfterSetupLocked(paths, censusMarker)
	if err != nil {
		return err
	}
	if len(censusMarker) > 0 && !reactivated {
		return fmt.Errorf("setup was superseded by an uninstall; rerun setup")
	}
	// Rotate only after the stopped marker was cleared successfully. A lock or
	// marker failure must leave the uninstall generation intact.
	return rotateInstallLifecycleGeneration(paths)
}

func mergeLatestSetupState(paths runtimePaths, state *installState) error {
	version := state.Version
	source := state.InstallSource
	latest, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return err
	}
	*state = latest
	state.Version = version
	state.InstallSource = source
	return nil
}

func installFileClientsForRepair(paths runtimePaths, state *installState, clients []string) error {
	return withClientMutationLock(paths, func() error {
		return installFileClientsForRepairUnlocked(paths, state, clients)
	})
}

func installFileClientsForRepairUnlocked(paths runtimePaths, state *installState, clients []string) error {
	return installClientsWithPolicy(paths, state, clients, true)
}

func withClientMutationLock(paths runtimePaths, mutate func() error) error {
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return fmt.Errorf("another HA NOVA client update is already in progress")
	}
	defer release()
	return mutate()
}

func withSetupLifecycleLock(paths runtimePaths, lifecycleMarker [][]byte, mutate func() error) error {
	return withClientMutationLock(paths, func() error {
		if err := ensureSetupLifecycleCurrent(paths, lifecycleMarker...); err != nil {
			return err
		}
		if err := mutate(); err != nil {
			return err
		}
		return refreshSetupConfigSnapshot(paths, lifecycleMarker)
	})
}

func installClientsWithPolicy(paths runtimePaths, state *installState, clients []string, allowFileClientWithoutRuntime bool) error {
	sourceRoot := resolveSourceRoot(paths)
	for _, client := range normalizeClients(clients) {
		mode, err := installClientWithPolicy(paths, sourceRoot, client, allowFileClientWithoutRuntime)
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
	return installClientWithPolicy(paths, sourceRoot, client, false)
}

func installClientWithPolicy(paths runtimePaths, sourceRoot, client string, allowFileClientWithoutRuntime bool) (string, error) {
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
	if !status.RuntimeDetected && !(allowFileClientWithoutRuntime && fileClientAdapter(entry.AdapterKind)) {
		return "", fmt.Errorf("%s is not available yet: %s", entry.Label, status.Reason)
	}
	switch entry.AdapterKind {
	case "skill_tree":
		switch client {
		case "codex":
			if err := requireSessionBootstrap(sourceRoot); err != nil {
				return "", err
			}
			return installTreeClient(filepath.Join(paths.Home, ".agents", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
		case "opencode":
			if err := requireSessionBootstrap(sourceRoot); err != nil {
				return "", err
			}
			return installTreeClient(filepath.Join(paths.Home, ".config", "opencode", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
		case "hermes":
			if err := requireSessionBootstrap(sourceRoot); err != nil {
				return "", err
			}
			return "copy", installHermesClient(paths.Home, sourceRoot)
		default:
			return "", fmt.Errorf("unsupported skill-tree client: %s", client)
		}
	case "skill_flat":
		if client != "antigravity" {
			return "", fmt.Errorf("unsupported skill-flat client: %s", client)
		}
		if err := requireSessionBootstrap(sourceRoot); err != nil {
			return "", err
		}
		return "copy", installAntigravityClientWithPolicy(paths.Home, sourceRoot, !allowFileClientWithoutRuntime)
	case "plugin_marketplace":
		if client != "claude" {
			return "", fmt.Errorf("unsupported plugin-marketplace client: %s", client)
		}
		if err := requireSessionBootstrap(sourceRoot); err != nil {
			return "", err
		}
		return "plugin", installClaudePlugin(paths, sourceRoot)
	default:
		return "", fmt.Errorf("unsupported adapter kind: %s", entry.AdapterKind)
	}
}

func fileClientAdapter(adapterKind string) bool {
	return adapterKind == "skill_tree" || adapterKind == "skill_flat"
}

func requireSessionBootstrap(sourceRoot string) error {
	path := filepath.Join(sourceRoot, "skills", "ha-nova", "session-bootstrap.md")
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return nil
	}
	if err == nil {
		err = fmt.Errorf("path is a directory")
	}
	return fmt.Errorf("invalid HA NOVA source: missing session bootstrap %s: %w", path, err)
}

func preferSymlinkForCurrentOS() bool {
	return runtime.GOOS != "windows"
}

func installTreeClient(parentDir, sourceDir string, preferSymlink bool) (string, error) {
	targetDir := filepath.Join(parentDir, "ha-nova")
	if preferSymlink {
		if err := replacePathAtomic(targetDir, func(stage string) error {
			return os.Symlink(sourceDir, stage)
		}); err == nil {
			return "symlink", nil
		}
	}
	if err := replacePathAtomic(targetDir, func(stage string) error {
		return copyDir(sourceDir, stage)
	}); err != nil {
		return "", err
	}
	return "copy", nil
}
