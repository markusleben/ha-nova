package main

import "path/filepath"

func detectInstalledClients(paths runtimePaths) ([]string, error) {
	clients := []string{}
	entries, err := loadClientRegistry(paths)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		status := evaluateClientStatus(paths, installState{}, entry)
		if entry.ID == "claude" {
			snapshot := inspectClaudeInstallSnapshot(paths, installState{})
			if status.RuntimeDetected && (status.Attached || snapshot.MarketplaceFound || snapshot.PluginFound) {
				clients = append(clients, entry.ID)
			}
			continue
		}
		if entry.ID == "hermes" {
			// Namespaced-context fingerprint keeps stale (pre-new-skill)
			// bundles detectable when state.json is absent, so the no-state
			// resync path can repair them.
			if managedInstalledFileClient(paths, entry.ID) &&
				(status.Attached || hermesNamespacedContextPresent(paths.Home) || hermesLegacyBundlePresent(paths.Home)) {
				clients = append(clients, entry.ID)
			}
			continue
		}
		if fileClientAdapter(entry.AdapterKind) && status.Attached && managedInstalledFileClient(paths, entry.ID) {
			clients = append(clients, entry.ID)
		}
	}
	return normalizeClients(clients), nil
}

// Runtime-independent repair is allowed only for a provably HA NOVA-managed
// layout. The adapters replace whole trees, so existence alone is not enough.
func managedInstalledFileClient(paths runtimePaths, client string) bool {
	switch client {
	case "codex":
		root := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
		managed, _ := managedSkillFingerprint(root, filepath.Join(root, "ha-nova", "SKILL.md"), true)
		return managed || symlinkTargetsTransientInstallBackup(root, paths.InstallRoot)
	case "opencode":
		root := filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
		managed, _ := managedSkillFingerprint(root, filepath.Join(root, "ha-nova", "SKILL.md"), true)
		return managed || symlinkTargetsTransientInstallBackup(root, paths.InstallRoot)
	case "antigravity":
		context := filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova", "SKILL.md")
		managed, _ := managedSkillFingerprint(context, context, false)
		return managed
	case "hermes":
		root := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
		namespaced := filepath.Join(root, "ha-nova", "SKILL.md")
		legacy := filepath.Join(root, "SKILL.md")
		namespacedManaged, _ := skillDeclaresHANova(namespaced)
		legacyManaged, _ := skillDeclaresHANova(legacy)
		return namespacedManaged || legacyManaged
	default:
		return false
	}
}
