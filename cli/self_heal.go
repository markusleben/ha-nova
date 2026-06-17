package main

// ensureClientsVerifiedForCurrentVersion re-syncs client integrations once after
// a version change, then records the version in state so it stays a no-op until
// the next change.
//
// It self-heals installs updated by a pre-0.6.1 binary: that updater ran the
// in-process swap, renaming the running binary into the soon-deleted
// `.ha-nova-old-*` backup, so client sync resolved its source from the stale
// backup (missing files, SKILL.md paths pointing at the backup). The 0.6.1 root
// fix (isTransientInstallBackup) prevents this going forward; this marker repairs
// the one-time v0.6.0->v0.6.1 transition for EVERY configured client.
//
// It is called from `check-update` and `doctor` — the shared update path every
// client (incl. Hermes) runs on first skill use per session (skills/ha-nova/
// SKILL.md), so it does not depend on the Claude-only session-start hook. Those
// commands are not parsed-stdout hot paths, so the per-client sync output is fine.
//
// Best-effort: it never aborts the caller. A version marker — not content
// detection — gates it, so it runs at most once per version and is near-free
// otherwise (one state read + compare).
func ensureClientsVerifiedForCurrentVersion(paths runtimePaths) {
	// Dev-synced builds manage their own client wiring; never clobber them.
	if BuildChannel == "dev" {
		return
	}
	version := localVersion(paths)
	if version == "" || version == "dev" {
		return
	}
	state := loadStateOrDefault(paths)
	// Pre-setup install (no recorded version): nothing to heal — setup stamps the
	// marker when it runs, and we must not create state before the user sets up.
	if state.Version == "" {
		return
	}
	// Steady state: marker matches the running version.
	if state.ClientsVerifiedVersion == version {
		return
	}
	// A dev INSTALL (release version.json + dev source tree) must also skip so
	// dev-synced clients are never overwritten.
	if normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev {
		return
	}
	// Serialize against the session-start auto-repair so the two never race on a
	// client's files. If another run holds the lock, skip — it (or the next
	// command) heals.
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		return
	}
	defer release()
	// Re-check under the lock: a concurrent run may have just stamped the marker.
	if loadStateOrDefault(paths).ClientsVerifiedVersion == version {
		return
	}
	// postUpdateSync stamps the marker after the pass (even on per-client failure),
	// so a persistently-failing client cannot retrigger a full re-sync next time.
	if err := postUpdateSync(paths); err != nil {
		printHumanWarn("Client self-heal incomplete (retries on next version change): %s", err)
	}
}
