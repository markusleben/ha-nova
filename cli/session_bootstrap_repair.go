package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sessionBootstrapPointer = "../ha-nova/session-bootstrap.md"
const sessionBootstrapVerifiedMarker = "session-bootstrap-layout-verified"
const sessionBootstrapRepairPendingFile = "session-bootstrap-repair-pending.json"
const sessionBootstrapCarrierPendingSuffix = ":carrier-pending"
const sessionBootstrapCarrierRunningPrefix = ":carrier-running:"
const sessionBootstrapCarrierClaimTTL = 5 * time.Minute
const sessionBootstrapCarrierMinBudget = 500 * time.Millisecond
const sessionBootstrapCarrierContentionWait = 500 * time.Millisecond

type sessionBootstrapRepairPending struct {
	Version string   `json:"version"`
	Clients []string `json:"clients"`
}

// repairMissingSessionBootstrap is the old-copy migration anchor. An update is
// applied by the already-running old binary, so its post-update sync cannot
// know about a newly mandatory skill file. Old copied skills do still call
// relay health/ws/core; the new binary repairs those exact file layouts here,
// without writing to relay stdout or trusting the generic version marker.
func repairMissingSessionBootstrap(paths runtimePaths) bool {
	candidate, _ := repairMissingSessionBootstrapWithContention(paths)
	return candidate
}

func repairMissingSessionBootstrapWithContention(paths runtimePaths) (bool, bool) {
	if BuildChannel == "dev" {
		return false, false
	}
	lifecycleGeneration, generationErr := readInstallLifecycleGeneration(paths)
	if generationErr != nil || censusLifecycleStopped(paths) {
		return false, false
	}
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return false, false
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	pendingPath := filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile)
	_, pendingExists, pendingErr := loadSessionBootstrapRepairPending(pendingPath, version)
	if pendingErr != nil {
		printHumanWarn("Session-bootstrap repair recovery plan is unreadable (repair stopped): %s", pendingErr)
		return false, false
	}
	if !pendingExists && markerHasVersion(marker, version) {
		return false, false
	}
	state := loadStateOrDefault(paths)
	if normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev {
		return false, false
	}
	sourceRoot := resolveSourceRoot(paths)
	if !fileExists(filepath.Join(sourceRoot, "skills", "ha-nova", "session-bootstrap.md")) {
		return false, false
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return false, true
	}
	defer release()
	if err := ensureUpdateLifecycleCurrent(paths, lifecycleGeneration); err != nil {
		return false, false
	}
	clients, pendingExists, pendingErr := loadSessionBootstrapRepairPending(pendingPath, version)
	if pendingErr != nil {
		printHumanWarn("Session-bootstrap repair recovery plan is unreadable (repair stopped): %s", pendingErr)
		return false, false
	}
	if !pendingExists && markerHasVersion(marker, version) {
		return false, false
	}
	if !pendingExists && (markerHasCarrierPending(marker, version) || markerHasCarrierRunning(marker, version)) {
		return true, false
	}
	if !pendingExists {
		migrate, active := olderCarrierIntentNeedsMigration(marker, version, time.Now())
		if active {
			return false, false
		}
		if migrate {
			if err := writeFileAtomic(
				marker,
				[]byte(version+sessionBootstrapCarrierPendingSuffix+"\n"),
				0o644,
			); err != nil {
				return false, false
			}
			return true, false
		}
	}
	if !pendingExists {
		var err error
		clients, err = sessionBootstrapRepairClients(paths, sourceRoot)
		if err != nil {
			printHumanWarn("Session-bootstrap repair could not inspect installed skills (retries on the next Relay command): %s", err)
			return false, false
		}
	}
	if len(clients) > 0 {
		if err := repairPlanTargetsSafe(paths, sourceRoot, clients); err != nil {
			printHumanWarn("Session-bootstrap repair recovery plan no longer owns its target (repair stopped): %s", err)
			return false, false
		}
		// This repair does not own setup/update state. Keeping it stateless
		// avoids replacing a concurrent writer's newer snapshot while client
		// files are rebuilt under the dedicated auto-repair lock.
		if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
			return false, false
		}
		if err := writeJSONFile(pendingPath, sessionBootstrapRepairPending{
			Version: version,
			Clients: clients,
		}, 0o600); err != nil {
			printHumanWarn("Session-bootstrap repair could not persist its recovery plan: %s", err)
			return false, false
		}
		if err := installFileClientsForRepairUnlocked(paths, nil, clients); err != nil {
			printHumanWarn("Session-bootstrap repair incomplete (retries on the next Relay command): %s", err)
			return false, false
		}
		if err := writeFileAtomic(
			marker,
			[]byte(version+sessionBootstrapCarrierPendingSuffix+"\n"),
			0o644,
		); err != nil {
			printHumanWarn("Session-bootstrap repair completed, but its first-use carrier could not be persisted: %s", err)
			return false, false
		}
		if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
			printHumanWarn("Session-bootstrap repair completed, but its recovery plan could not be cleared: %s", err)
			return false, false
		}
		return true, false
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return false, false
	}
	if err := writeFileAtomic(marker, []byte(version+"\n"), 0o644); err != nil {
		return false, false
	}
	return false, false
}

func markerHasVersion(path, version string) bool {
	raw, ok := readSessionBootstrapMarker(path)
	return ok && raw == version
}

func markerHasCarrierPending(path, version string) bool {
	raw, ok := readSessionBootstrapMarker(path)
	return ok && raw == version+sessionBootstrapCarrierPendingSuffix
}

func markerHasCarrierRunning(path, version string) bool {
	raw, ok := readSessionBootstrapMarker(path)
	return ok && strings.HasPrefix(raw, version+sessionBootstrapCarrierRunningPrefix)
}

func olderCarrierIntentNeedsMigration(path, currentVersion string, now time.Time) (migrate, active bool) {
	raw, ok := readSessionBootstrapMarker(path)
	if !ok {
		return false, false
	}
	if strings.HasSuffix(raw, sessionBootstrapCarrierPendingSuffix) {
		version := strings.TrimSuffix(raw, sessionBootstrapCarrierPendingSuffix)
		if version == currentVersion {
			return false, false
		}
		if !canonicalCarrierVersion(version) {
			return false, false
		}
		return true, false
	}
	index := strings.LastIndex(raw, sessionBootstrapCarrierRunningPrefix)
	if index <= 0 {
		return false, false
	}
	version := raw[:index]
	if version == currentVersion {
		return false, false
	}
	if !canonicalCarrierVersion(version) {
		return false, false
	}
	timestamp := raw[index+len(sessionBootstrapCarrierRunningPrefix):]
	unix, parseErr := strconv.ParseInt(timestamp, 10, 64)
	if parseErr != nil || unix < 0 || strconv.FormatInt(unix, 10) != timestamp {
		return true, false
	}
	elapsed := now.Sub(time.Unix(unix, 0))
	if elapsed >= 0 && elapsed < sessionBootstrapCarrierClaimTTL {
		return false, true
	}
	return true, false
}

func canonicalCarrierVersion(version string) bool {
	parsed, err := parseReleaseVersion(version)
	if err != nil {
		return false
	}
	canonical := fmt.Sprintf("%d.%d.%d", parsed.Major, parsed.Minor, parsed.Patch)
	if parsed.RC > 0 {
		canonical += fmt.Sprintf("-rc%d", parsed.RC)
	}
	return version == canonical
}

func loadSessionBootstrapRepairPending(path, version string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var pending sessionBootstrapRepairPending
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, true, fmt.Errorf("invalid JSON: %w", err)
	}
	allowed := map[string]bool{
		"codex": true, "opencode": true, "antigravity": true, "hermes": true,
	}
	clients := []string{}
	for _, client := range normalizeClients(pending.Clients) {
		if allowed[client] {
			clients = append(clients, client)
		}
	}
	if len(clients) == 0 {
		return nil, true, fmt.Errorf("plan contains no supported clients")
	}
	// A process can die after persisting the plan and before a later release is
	// installed. Client IDs are version-independent; resume them from the
	// current source and rewrite the plan to the running version before mutation.
	return clients, true, nil
}

func markSessionBootstrapLayoutVerified(paths runtimePaths) {
	lifecycleGeneration, generationErr := readInstallLifecycleGeneration(paths)
	if generationErr != nil || censusLifecycleStopped(paths) {
		return
	}
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return
	}
	defer release()
	if err := ensureUpdateLifecycleCurrent(paths, lifecycleGeneration); err != nil {
		return
	}
	_, pendingExists, pendingErr := loadSessionBootstrapRepairPending(
		filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile),
		version,
	)
	if pendingErr != nil || pendingExists {
		return
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if markerHasCarrierPending(marker, version) || markerHasCarrierRunning(marker, version) {
		return
	}
	if migrate, active := olderCarrierIntentNeedsMigration(marker, version, time.Now()); migrate || active {
		return
	}
	sourceRoot := resolveSourceRoot(paths)
	stale, err := sessionBootstrapRepairClients(paths, sourceRoot)
	if err != nil || len(stale) > 0 {
		return
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return
	}
	_ = writeFileAtomic(marker, []byte(version+"\n"), 0o644)
}
