package main

// allTrackedClientsSynced reports whether every client currently tracked in
// state was part of the just-synced set.
func allTrackedClientsSynced(tracked, synced []string) bool {
	syncedSet := make(map[string]struct{}, len(synced))
	for _, client := range synced {
		syncedSet[client] = struct{}{}
	}
	for _, client := range tracked {
		if _, ok := syncedSet[client]; !ok {
			return false
		}
	}
	return true
}

// ensureClientsVerifiedForCurrentVersion re-syncs tracked client integrations
// once after a release change. It repairs the historical transient-backup
// source bug and stays best-effort so update discovery remains advisory.
func ensureClientsVerifiedForCurrentVersion(paths runtimePaths) {
	if BuildChannel == "dev" {
		return
	}
	lifecycleGeneration, generationErr := readInstallLifecycleGeneration(paths)
	if generationErr != nil {
		return
	}
	if censusLifecycleStopped(paths) {
		return
	}
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return
	}
	state := loadStateOrDefault(paths)
	if state.Version == "" || state.ClientsVerifiedVersion == version {
		return
	}
	if normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev {
		return
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return
	}
	defer release()
	if err := ensureUpdateLifecycleCurrent(paths, lifecycleGeneration); err != nil || censusLifecycleStopped(paths) {
		return
	}
	currentState := loadStateOrDefault(paths)
	if currentState.Version == "" || currentState.ClientsVerifiedVersion == version {
		return
	}
	if err := postUpdateSyncWithResultUnlocked(paths).Err; err != nil {
		printHumanWarn("Client self-heal incomplete (retries on next run): %s", err)
	}
}
