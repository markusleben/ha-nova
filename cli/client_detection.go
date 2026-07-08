package main

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
			if status.RuntimeDetected && (status.Attached || hermesNamespacedContextPresent(paths.Home) || hermesLegacyBundlePresent(paths.Home)) {
				clients = append(clients, entry.ID)
			}
			continue
		}
		if status.Attached && status.RuntimeDetected {
			clients = append(clients, entry.ID)
		}
	}
	return normalizeClients(clients), nil
}
