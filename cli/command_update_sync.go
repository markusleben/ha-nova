package main

import (
	"fmt"
	"strings"
)

type postUpdateSyncResult struct {
	FullySynced      bool
	RefreshedClients bool
	Err              error
}

// postUpdateSync re-syncs every configured client from the canonical install root
// and, when the whole set verified cleanly, stamps the client-verification marker
// (state.ClientsVerifiedVersion) that gates the post-update self-heal (see
// ensureClientsVerifiedForCurrentVersion).
//
// The marker is stamped only when EVERY configured client was actually synced —
// not when one was skipped, failed, or still references a transient backup.
// Already attached file clients are safe to refresh even when their runtime is
// temporarily absent; this prevents an old copied skill from missing the very
// first-use check that would otherwise heal it later. Initial installs and
// plugin-marketplace mutations remain runtime-gated. An unresolvable client
// leaves the marker unset for the next check-update/doctor attempt.
//
// The residue scan is the in-tool guarantee behind "Updated == clean": if a sync
// ever resolves from a stale backup, the user gets a loud warning + recovery hint
// instead of a silent success. For a fixed (>=0.6.2) binary it never fires.
func postUpdateSync(paths runtimePaths) error {
	return postUpdateSyncWithResult(paths).Err
}

func postUpdateSyncWithResult(paths runtimePaths) postUpdateSyncResult {
	lifecycleMarker, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		return postUpdateSyncResult{Err: fmt.Errorf("cannot inspect install lifecycle: %w", err)}
	}
	if censusLifecycleStopped(paths) {
		return postUpdateSyncResult{Err: fmt.Errorf("HA NOVA was uninstalled; run `ha-nova setup` before syncing clients")}
	}
	return postUpdateSyncWithResultForLifecycle(paths, lifecycleMarker)
}

func postUpdateSyncWithResultForLifecycle(paths runtimePaths, lifecycleMarker []byte) postUpdateSyncResult {
	var result postUpdateSyncResult
	err := withClientMutationLock(paths, func() error {
		if err := ensureUpdateLifecycleCurrent(paths, lifecycleMarker); err != nil {
			return err
		}
		result = postUpdateSyncWithResultUnlocked(paths)
		return result.Err
	})
	if err != nil {
		result.Err = err
	}
	return result
}

func postUpdateSyncWithResultUnlocked(paths runtimePaths) postUpdateSyncResult {
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return postUpdateSyncResult{Err: err}
	}
	state := loadStateOrDefault(paths)
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	failed := []string{}
	skipped := false
	syncedClients := 0
	for _, client := range configured {
		entry, ok, err := findRegistryClient(paths, client)
		if err != nil {
			return postUpdateSyncResult{Err: err}
		}
		if !ok {
			continue
		}
		status := evaluateClientStatus(paths, state, entry)
		canSyncAttachedFiles := status.Attached &&
			fileClientAdapter(entry.AdapterKind) &&
			managedInstalledFileClient(paths, client)
		if !status.RuntimeDetected && !canSyncAttachedFiles {
			printHumanWarn("Skipping %s until the client runtime is installed in this environment", entry.Label)
			skipped = true
			continue
		}
		if fileClientAdapter(entry.AdapterKind) {
			if err := repairPlanTargetsSafe(paths, resolveSourceRoot(paths), []string{client}); err != nil {
				printHumanWarn("Client sync refused: %s (%s)", client, err)
				failed = append(failed, client)
				continue
			}
		}
		if err := installFileClientsForRepairUnlocked(paths, &state, []string{client}); err != nil {
			printHumanWarn("Client sync failed: %s (%s)", client, err)
			failed = append(failed, client)
			continue
		}
		printHumanInfo("Client synced: %s", client)
		syncedClients++
	}
	state.InstalledClients = configured
	version := localVersion(paths)
	state.Version = version
	// Verify the just-synced trees are actually clean before marking the version
	// verified: a residue scan catches a sync that resolved from a transient backup
	// (the pre-0.6.1 bug class), so "Updated" never silently means "updated-ish".
	residue := transientBackupResidue(paths, configured)
	fullySynced := len(failed) == 0 && !skipped && len(residue) == 0
	if fullySynced {
		state.ClientsVerifiedVersion = version
	} else if len(residue) > 0 {
		// Residue proves the installed client tree is stale. Clear a matching marker
		// so the next check-update or doctor run retries the repair.
		state.ClientsVerifiedVersion = ""
	}
	if err := saveState(paths, state); err != nil {
		return postUpdateSyncResult{Err: err}
	}
	if len(residue) > 0 {
		printHumanWarn("These clients still reference a temporary update backup and are not fully up to date: %s", strings.Join(normalizeClients(residue), ", "))
		printHumanWarn("Run `ha-nova doctor` to refresh them.")
	}
	if len(failed) > 0 {
		return postUpdateSyncResult{Err: fmt.Errorf("failed clients: %s", strings.Join(normalizeClients(failed), ", "))}
	}
	return postUpdateSyncResult{
		FullySynced:      fullySynced,
		RefreshedClients: syncedClients > 0 && len(residue) == 0,
	}
}
