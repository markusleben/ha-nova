package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

type postUpdateSyncResult struct {
	FullySynced      bool
	RefreshedClients bool
	Err              error
	// Synced and Skipped name the AI apps (labels) for the one-line summary.
	Synced  []string
	Skipped []string
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
	if installLifecycleStopped(paths) {
		return postUpdateSyncResult{Err: fmt.Errorf("HA NOVA is not set up on this computer yet; run: ha-nova setup")}
	}
	return postUpdateSyncWithResultForLifecycle(paths, lifecycleMarker)
}

func postUpdateSyncWithResultForLifecycle(paths runtimePaths, lifecycleMarker []byte) postUpdateSyncResult {
	var result postUpdateSyncResult
	err := withClientMutationLock(paths, func() error {
		if err := ensureUpdateLifecycleCurrent(paths, lifecycleMarker); err != nil {
			return err
		}
		result = postUpdateSyncWithResultUnlocked(paths, false)
		return result.Err
	})
	if err != nil {
		result.Err = err
	}
	return result
}

// automatic: a self-heal nobody asked for, which leaves a Claude Code that
// kept an older HA NOVA through a repair to setup and update.
func postUpdateSyncWithResultUnlocked(paths runtimePaths, automatic bool) postUpdateSyncResult {
	detectedClients, err := detectInstalledClients(paths)
	if err != nil {
		return postUpdateSyncResult{Err: err}
	}
	state := loadStateOrDefault(paths)
	configured := normalizeClients(append(append([]string{}, state.InstalledClients...), detectedClients...))
	failed := []string{}
	var synced, syncedIDs, skipped, unverified []string
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
			skipped = append(skipped, entry.Label)
			continue
		}
		if fileClientAdapter(entry.AdapterKind) {
			if err := repairPlanTargetsSafe(paths, resolveSourceRoot(paths), []string{client}); err != nil {
				printHumanWarn("Client sync refused: %s (%s)", client, err)
				failed = append(failed, client)
				continue
			}
		}
		if automatic && client == "claude" && claudeRepairStuck(paths, state) {
			unverified = append(unverified, entry.Label)
			continue
		}
		err = installFileClientsForRepairUnlocked(paths, &state, []string{client})
		if client == "claude" {
			err = noteClaudeRepairAfter(paths, state, err)
		}
		if errors.Is(err, errClaudeKeptOlderPlugin) { // not a failed update: doctor names the fix
			unverified = append(unverified, entry.Label)
			continue
		}
		if err != nil {
			printHumanWarn("Client sync failed: %s (%s)", client, err)
			failed = append(failed, client)
			continue
		}
		// Verify the version: Claude Code's plugin must now point at this
		// release's snapshot. File clients link or copy the one install root,
		// so the residue scan below is their check.
		if !fileClientAdapter(entry.AdapterKind) && !clientAttachmentPresentForStatus(paths, state, client) {
			unverified = append(unverified, entry.Label)
			continue
		}
		synced = append(synced, entry.Label)
		syncedIDs = append(syncedIDs, client)
	}
	state.InstalledClients = configured
	version := localVersion(paths)
	state.Version = version
	// Verify the just-synced trees are actually clean before marking the version
	// verified: a residue scan catches a sync that resolved from a transient backup
	// (the pre-0.6.1 bug class), so "Updated" never silently means "updated-ish".
	residue := transientBackupResidue(paths, configured)
	fullySynced := len(failed) == 0 && len(skipped) == 0 && len(unverified) == 0 && len(residue) == 0
	if fullySynced {
		state.ClientsVerifiedVersion = version
	} else if len(residue) > 0 || len(unverified) > 0 {
		// Residue or a failed attachment check proves a client is stale. Clear a
		// matching marker so the next check-update or doctor run retries the repair.
		state.ClientsVerifiedVersion = ""
	}
	if err := saveState(paths, state); err != nil {
		return postUpdateSyncResult{Err: err}
	}
	if len(unverified) > 0 {
		printHumanWarn("Not ready in %s after the update. Run: ha-nova doctor", joinAnd(unverified))
	}
	if len(skipped) > 0 {
		printHumanWarn("Not updated in %s: it isn't on this computer right now. If you use it again, run: ha-nova update", joinAnd(skipped))
	}
	if len(residue) > 0 {
		printHumanWarn("Not fully updated in %s (they still point at a temporary update copy). Run: ha-nova doctor", strings.Join(normalizeClients(residue), ", "))
	}
	// The summary names only the apps that are fully on this version.
	for i := len(syncedIDs) - 1; i >= 0; i-- {
		if containsClient(residue, syncedIDs[i]) {
			synced = append(synced[:i], synced[i+1:]...)
		}
	}
	if len(failed) > 0 {
		return postUpdateSyncResult{Err: fmt.Errorf("failed clients: %s", strings.Join(normalizeClients(failed), ", "))}
	}
	return postUpdateSyncResult{
		FullySynced:      fullySynced,
		RefreshedClients: len(synced) > 0 && len(residue) == 0,
		Synced:           synced,
		Skipped:          skipped,
	}
}

// printUpdateDone: one plain sentence for what the update did, naming every
// AI app it refreshed (the sync already warned about the others), and where
// "Codex" works.
func printUpdateDone(lead string, result postUpdateSyncResult) {
	if len(result.Synced) > 0 {
		printHumanInfo("%s in %s.", lead, joinAnd(result.Synced))
		if slices.Contains(result.Synced, setupClientLabel("codex")) {
			printHumanInfo("%s", codexWhereLine())
		}
		return
	}
	printHumanInfo("%s.", lead)
}

// openSetupPageForAppUpdate runs the NEW binary's `ha-nova setup`: its
// re-run page leads with "Update the NOVA App", and nothing installs until
// the owner clicks and signs in. Test seam.
var openSetupPageForAppUpdate = func(paths runtimePaths) error {
	cmd := exec.Command(paths.PublicBinary, "setup")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// relayUpdateNoticeForUpdate: the App's state after the CLI update. Test seam.
var relayUpdateNoticeForUpdate = relayUpdateNotice

// offerRelayUpdateAfterUpdate is the NOVA App half of `ha-nova update`.
func offerRelayUpdateAfterUpdate(paths runtimePaths) {
	offerRelayUpdate(paths, relayUpdateNoticeForUpdate(paths))
}

// setupPageCanUpdateApp: `ha-nova setup` opens its page here, and the page's
// owner sign-in can update the official App on this Home Assistant.
func setupPageCanUpdateApp(paths runtimePaths) bool {
	cfg, err := loadConfig(paths)
	if err != nil {
		return false
	}
	_, originErr := parseLocalHAOrigin(cfg.HAURL)
	return originErr == nil && setupPageCloudOK(cfg) && haAppInstallTargetsThisHAForPage(cfg)
}

// offerRelayUpdate: an App below what this version needs, with its update
// pending in Home Assistant, at a desktop: the setup page opens on its
// "Update the NOVA App" button. In a terminal otherwise: the guided offer
// (it asks first). Without a terminal (an AI app ran the command): only the
// notice, which says where to update it. Nothing installs without the
// user's click or yes.
func offerRelayUpdate(paths runtimePaths, notice humanNotice) {
	if notice.empty() {
		return
	}
	printHumanNotice(notice)
	if notice.belowFloor && serverSelectionOverride == "" &&
		activeServerProfile() == defaultServerProfileName && setupPageCanUpdateApp(paths) && setupPageAvailable() {
		printHumanInfo("Opening HA NOVA setup: click \"Update the NOVA App\" there.")
		if err := openSetupPageForAppUpdate(paths); err == nil {
			if relayUpdateNoticeForUpdate(paths).empty() {
				return
			}
			printHumanInfo("Not updated yet. %s", relayAppUpdatePath) // closed without updating: where to do it later
			return
		}
	}
	if relayUpdateInputIsTTY() && relayUpdateOutputIsTTY() {
		maybeOfferGuidedRelayUpdate(paths, notice) // says the manual path itself when it can't
	} // else the notice already said where to update it
}
