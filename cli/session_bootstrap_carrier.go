package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func claimSessionBootstrapCarrier(paths runtimePaths) bool {
	return claimSessionBootstrapCarrierUntil(paths, time.Now())
}

func claimSessionBootstrapCarrierUntil(paths runtimePaths, deadline time.Time) bool {
	_, _, claimed := claimSessionBootstrapCarrierValueUntil(paths, deadline)
	return claimed
}

func claimSessionBootstrapCarrierValueUntil(paths runtimePaths, deadline time.Time) (string, []byte, bool) {
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return "", nil, false
	}
	generation, err := readInstallLifecycleGeneration(paths)
	if err != nil || censusLifecycleStopped(paths) {
		return "", nil, false
	}
	release, acquired := acquireAutoRepairLockUntil(paths, deadline)
	if !acquired {
		return "", nil, false
	}
	defer release()
	if ensureUpdateLifecycleCurrent(paths, generation) != nil {
		return "", nil, false
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	claimable := markerHasCarrierPending(marker, version)
	if markerHasCarrierRunning(marker, version) {
		startedAt, ok := sessionBootstrapCarrierStartedAt(marker, version)
		elapsed := time.Since(startedAt)
		if ok && elapsed >= 0 && elapsed < sessionBootstrapCarrierClaimTTL {
			return "", nil, false
		}
		claimable = true
	}
	if !claimable {
		return "", nil, false
	}
	value := fmt.Sprintf("%s%s%d", version, sessionBootstrapCarrierRunningPrefix, time.Now().Unix())
	if writeFileAtomic(marker, []byte(value+"\n"), 0o644) != nil {
		return "", nil, false
	}
	return value, generation, true
}

func sessionBootstrapCarrierStartedAt(path, version string) (time.Time, bool) {
	raw, ok := readSessionBootstrapMarker(path)
	if !ok {
		return time.Time{}, false
	}
	prefix := version + sessionBootstrapCarrierRunningPrefix
	if !strings.HasPrefix(raw, prefix) {
		return time.Time{}, false
	}
	timestamp := strings.TrimPrefix(raw, prefix)
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || unix < 0 || strconv.FormatInt(unix, 10) != timestamp {
		return time.Time{}, false
	}
	return time.Unix(unix, 0), true
}

func readSessionBootstrapMarker(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(data), "\r\n"), true
}

// runMigratedFirstUseCheck closes the one-session rollout gap for skills copied
// by an older updater. Their already-loaded prompt cannot observe the repaired
// bootstrap until the next session, so the new binary carries the same
// update/census check once. Notices use stderr to preserve Relay JSON stdout.
func runMigratedFirstUseCheck(paths runtimePaths, budget time.Duration) {
	if budget <= 0 {
		budget = time.Duration(defaultRelayMaxTimeSeconds * float64(time.Second))
	}
	connectBudget := time.Duration(defaultRelayConnectTimeoutSeconds * float64(time.Second))
	if connectBudget > budget {
		connectBudget = budget
	}
	client := newRelayHTTPClient(connectBudget.Seconds(), budget.Seconds())
	updateResult := make(chan updateCheckResult, 1)
	relayResult := make(chan humanNotice, 1)
	go func() {
		updateResult <- buildUpdateCheckResultWithClient(paths, client)
	}()
	go func() {
		relayResult <- relayUpdateNoticeWithTimeout(paths, budget)
	}()
	result := <-updateResult
	if notice := humanNoticeFromUpdateCheckResult(result, true); !notice.empty() {
		printHumanNotice(notice)
	}
	if relayNotice := <-relayResult; !relayNotice.empty() {
		printHumanNotice(relayNotice)
	}
	// Do not add a census network wait beyond the caller's explicit Relay
	// budget. Opted-in installs resume the weekly carrier in the repaired
	// bootstrap's next check-update invocation.
}

var runMigratedFirstUseCheckForCarrier = runMigratedFirstUseCheck

func finishMigratedFirstUse(paths runtimePaths, candidate, contended bool, deadline time.Time) {
	if !candidate && !contended {
		return
	}
	requiredBudget := firstUseRelayNoticeTimeout + censusLockTimeout + sessionBootstrapCarrierMinBudget
	claimDeadline := deadline.Add(-requiredBudget)
	if !candidate {
		contentionDeadline := time.Now().Add(sessionBootstrapCarrierContentionWait)
		if contentionDeadline.Before(claimDeadline) {
			claimDeadline = contentionDeadline
		}
	}
	if time.Until(claimDeadline) <= 0 {
		return
	}
	claimValue, claimGeneration, claimed := claimSessionBootstrapCarrierValueUntil(paths, claimDeadline)
	if !claimed {
		return
	}
	remaining := time.Until(deadline)
	carrierBudget := remaining
	if remaining > censusLockTimeout {
		carrierBudget = remaining - censusLockTimeout
	}
	if carrierBudget > firstUseRelayNoticeTimeout {
		carrierBudget = firstUseRelayNoticeTimeout
	}
	runMigratedFirstUseCheckForCarrier(paths, carrierBudget)
	completeMigratedFirstUseClaim(paths, claimValue, claimGeneration, deadline)
}

func completeMigratedFirstUseClaim(paths runtimePaths, claimValue string, generation []byte, deadline time.Time) {
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return
	}
	release, acquired := acquireAutoRepairLockUntil(paths, deadline)
	if !acquired {
		return
	}
	defer release()
	if ensureUpdateLifecycleCurrent(paths, generation) != nil {
		return
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if raw, ok := readSessionBootstrapMarker(marker); !ok || raw != claimValue {
		return
	}
	if time.Until(deadline) <= censusLockTimeout || !maybeEmitCensusSkillNoticeTo(paths, os.Stderr) {
		_ = writeFileAtomic(marker, []byte(version+sessionBootstrapCarrierPendingSuffix+"\n"), 0o644)
		return
	}
	_ = writeFileAtomic(marker, []byte(version+"\n"), 0o644)
}

func resetSessionBootstrapCarrier(paths runtimePaths, claimValue string, deadline time.Time) {
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return
	}
	release, acquired := acquireAutoRepairLockUntil(paths, deadline)
	if !acquired {
		return
	}
	defer release()
	if censusLifecycleStopped(paths) {
		return
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if raw, ok := readSessionBootstrapMarker(marker); ok && raw == claimValue {
		_ = writeFileAtomic(marker, []byte(version+sessionBootstrapCarrierPendingSuffix+"\n"), 0o644)
	}
}

func finalizeSessionBootstrapCarrier(paths runtimePaths, claimValue string, deadline time.Time) bool {
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return false
	}
	release, acquired := acquireAutoRepairLockUntil(paths, deadline)
	if !acquired {
		return false
	}
	defer release()
	if censusLifecycleStopped(paths) {
		return false
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if raw, ok := readSessionBootstrapMarker(marker); !ok || raw != claimValue {
		return false
	}
	return writeFileAtomic(marker, []byte(version+"\n"), 0o644) == nil
}

func finalizePendingSessionBootstrapCarrier(paths runtimePaths) bool {
	return finalizePendingSessionBootstrapCarrierUntil(paths, time.Now())
}

func finalizePendingSessionBootstrapCarrierUntil(paths runtimePaths, deadline time.Time) bool {
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return false
	}
	release, acquired := acquireAutoRepairLockUntil(paths, deadline)
	if !acquired {
		return false
	}
	defer release()
	if censusLifecycleStopped(paths) {
		return false
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if !markerHasCarrierPending(marker, version) {
		return false
	}
	return writeFileAtomic(marker, []byte(version+"\n"), 0o644) == nil
}
