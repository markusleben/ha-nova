package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const censusStateSchemaVersion = 1

// censusNow is the census clock, overridable for tests (ISO-week gate,
// relay-freshness and throttle tests).
var censusNow = time.Now

// censusState is census.json next to config.json — per-install, profile
// independent, removed by uninstall. It records the one-time ask (never-nag),
// the explicit opt-in, the ISO-week send gate, and the opportunistically
// observed relay version.
type censusState struct {
	Schema       int    `json:"schema"`
	AskedAt      string `json:"asked_at,omitempty"`
	AskedVia     string `json:"asked_via,omitempty"`
	Answer       string `json:"answer,omitempty"` // yes | no | none
	Enabled      bool   `json:"enabled"`
	LastPingWeek string `json:"last_ping_week,omitempty"`
	SkillNotices int    `json:"skill_notices,omitempty"`
	// Relay version stamped from normal relay traffic (checkRelayVersionValue
	// funnel) — the census NEVER makes its own relay call.
	RelayVersion           string `json:"relay_version,omitempty"`
	RelayVersionObservedAt string `json:"relay_version_observed_at,omitempty"`
}

func defaultCensusState() censusState {
	return censusState{Schema: censusStateSchemaVersion}
}

// loadCensusState is best-effort: a missing or unreadable file means the
// default state (disabled, never asked). The census must never turn a broken
// file into a failed command. A file that EXISTS but cannot be parsed (or
// carries a future schema) is replaced with a stamped safe default instead —
// see recoverCensusState.
func loadCensusState(paths runtimePaths) censusState {
	if paths.CensusFile == "" {
		return defaultCensusState()
	}
	data, err := os.ReadFile(paths.CensusFile)
	if err != nil {
		return defaultCensusState()
	}
	var state censusState
	if err := json.Unmarshal(data, &state); err != nil {
		return recoverCensusState(paths)
	}
	if state.Schema > censusStateSchemaVersion {
		return recoverCensusState(paths)
	}
	if state.Schema == 0 {
		state.Schema = censusStateSchemaVersion
	}
	return state
}

// recoverCensusState replaces an unparsable or future-schema census.json with
// a persisted, stamped, disabled default: a corrupted file must never cause a
// re-ask nag (asked_at is set) or a silent re-enable (enabled stays false).
// Overwriting a future schema is deliberate — when an older CLI meets a newer
// file, disabling is the safe direction. It saves WITHOUT the census lock (it
// may already run under it via mutateCensusState, and the lock is not
// reentrant); that is acceptable because the written value is a constant safe
// default — concurrent recoveries are idempotent.
func recoverCensusState(paths runtimePaths) censusState {
	state := defaultCensusState()
	state.AskedAt = censusNow().UTC().Format(time.RFC3339)
	state.AskedVia = "recovered"
	state.Answer = "none"
	_ = saveCensusState(paths, state)
	return state
}

// Census state lock: reload-before-save alone cannot make an explicit opt-out
// WIN against a concurrent full read-modify-write cycle (a writer that loaded
// enabled=true before `census off` saved could restore it). The lock makes
// each load+mutate+save cycle mutually exclusive across processes. Vars so
// tests can shrink the timings.
var (
	censusLockRetryInterval = 50 * time.Millisecond
	censusLockTimeout       = 2 * time.Second
	// A crashed process must never wedge the census forever: a lock older
	// than this is treated as abandoned and taken over.
	censusLockStaleAfter = 10 * time.Second
)

// acquireCensusLock claims ConfigDir/census.lock via O_CREATE|O_EXCL with a
// bounded retry loop. It returns a release func and whether the lock was won.
// Environmental failures (no ConfigDir, cannot create files at all) degrade
// to "proceed unlocked" — a broken filesystem must not brick census commands,
// and the per-field mutations stay safe under the reload-before-save rule.
// NOT reentrant: never call while already holding the lock.
func acquireCensusLock(paths runtimePaths) (func(), bool) {
	unlocked := func() {}
	if paths.ConfigDir == "" {
		return unlocked, true
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return unlocked, true
	}
	lockPath := filepath.Join(paths.ConfigDir, "census.lock")
	deadline := time.Now().Add(censusLockTimeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(lockPath) }, true
		}
		if !errors.Is(err, os.ErrExist) {
			return unlocked, true
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > censusLockStaleAfter {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return unlocked, false
		}
		time.Sleep(censusLockRetryInterval)
	}
}

// mutateCensusState is the ONLY read-modify-write path for census writers: it
// holds the census lock around load+mutate+save, and callers mutate only
// their own fields. Together this serializes whole cycles across processes
// (so `census off` always wins) AND keeps concurrent writers from clobbering
// each other's fields. Lock ordering: the O_EXCL action markers
// (census-ping-*/census-notice-*) are always claimed OUTSIDE this lock, and
// the lock is never held across network I/O — no nesting, no deadlock.
func mutateCensusState(paths runtimePaths, mutate func(*censusState)) error {
	release, ok := acquireCensusLock(paths)
	if !ok {
		return fmt.Errorf("census state is locked by another process")
	}
	defer release()
	state := loadCensusState(paths)
	mutate(&state)
	return saveCensusState(paths, state)
}

func saveCensusState(paths runtimePaths, state censusState) error {
	if paths.CensusFile == "" {
		return fmt.Errorf("census state path unknown")
	}
	state.Schema = censusStateSchemaVersion
	return writeJSONFile(paths.CensusFile, state, 0o600)
}

// censusISOWeek renders the UTC ISO-8601 week label, zero-padded so that
// string order equals chronological order ("2026-W05" < "2026-W31").
func censusISOWeek(t time.Time) string {
	year, week := t.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func censusOptedOutByEnv() bool {
	return strings.TrimSpace(os.Getenv(censusOptOutEnv)) != ""
}

const censusRelayStampInterval = 24 * time.Hour

// stampCensusRelayVersion opportunistically records the relay version at the
// checkRelayVersionValue funnel (every /health body and relay response header
// passes through there). Hot-path protection: it writes only when the value
// changed or the stamp is older than 24h, and only for opted-in installs —
// otherwise it does nothing at all.
func stampCensusRelayVersion(paths runtimePaths, version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}
	state := loadCensusState(paths)
	if !state.Enabled {
		return
	}
	now := censusNow().UTC()
	if state.RelayVersion == version && state.RelayVersionObservedAt != "" {
		if observed, err := time.Parse(time.RFC3339, state.RelayVersionObservedAt); err == nil {
			age := now.Sub(observed)
			if age >= 0 && age < censusRelayStampInterval {
				return
			}
		}
	}
	// Write via the reload-mutate path and touch ONLY the relay fields — a
	// week stamp written between our load and this save must survive.
	_ = mutateCensusState(paths, func(s *censusState) {
		s.RelayVersion = version
		s.RelayVersionObservedAt = now.Format(time.RFC3339)
	})
}
