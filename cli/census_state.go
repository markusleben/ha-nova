package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const censusStateSchemaVersion = 1

// censusNow is the census clock, overridable for tests (ISO-week gate,
// relay-freshness and throttle tests).
var censusNow = time.Now

// censusState is per-device and profile-independent (under LOCALAPPDATA on
// Windows; next to config.json elsewhere), and removed by uninstall. It records the one-time ask (never-nag),
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

// loadCensusState is best-effort and strictly read-only: a missing or
// unreadable file means the default state (disabled, never asked). A file that
// exists but cannot be parsed (or carries a future schema) yields a stamped,
// disabled recovery value in memory. Only locked writer paths may persist it.
func loadCensusState(paths runtimePaths) censusState {
	state, _ := readCensusState(paths)
	return state
}

// readCensusState also reports whether an old client may safely write the
// loaded schema. Corrupt, unreadable, future-schema, and lifecycle-stopped
// state is safe to inspect as disabled but must never be overwritten.
func readCensusState(paths runtimePaths) (censusState, bool) {
	if censusLifecycleStopped(paths) {
		return recoverCensusState(), false
	}
	if paths.CensusFile == "" {
		return defaultCensusState(), false
	}
	data, err := os.ReadFile(paths.CensusFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultCensusState(), true
		}
		return recoverCensusState(), false
	}
	var state censusState
	if err := json.Unmarshal(data, &state); err != nil {
		return recoverCensusState(), false
	}
	if state.Schema > censusStateSchemaVersion {
		return recoverCensusState(), false
	}
	if state.Schema == 0 {
		state.Schema = censusStateSchemaVersion
	}
	return state, true
}

// recoverCensusState returns a stamped, disabled default without writing. That
// prevents an unlocked status/ask read from overwriting a concurrent consent
// or week-stamp mutation. The non-empty ask stamp also prevents a corrupt or
// future-schema file from causing repeated prompts.
func recoverCensusState() censusState {
	state := defaultCensusState()
	state.AskedAt = censusNow().UTC().Format(time.RFC3339)
	state.AskedVia = "recovered"
	state.Answer = "none"
	return state
}

// Census state lock: reload-before-save alone cannot make an explicit opt-out
// WIN against a concurrent full read-modify-write cycle (a writer that loaded
// enabled=true before `census off` saved could restore it). The OS advisory
// lock makes each cycle mutually exclusive across processes and is also held
// across the one bounded census POST, so a successful `census off` cannot
// return before an already-started send finishes. Vars so tests can shrink
// the timings.
var (
	censusLockRetryInterval = 50 * time.Millisecond
	// Longer than the hard 1.5-second request deadline plus filesystem and
	// scheduler margin. A crashed process releases an OS lock automatically.
	censusLockTimeout = 3 * time.Second
	// flock semantics on macOS are process-scoped, so separate descriptors in
	// one CLI process do not serialize goroutines. Take this bounded local lock
	// before the cross-process platform lock. One global coordinator is enough:
	// census work is rare and bounded to a single 1.5-second request.
	censusProcessLock        = make(chan struct{}, 1)
	censusPassiveLockTimeout = time.Millisecond
)

// acquireCensusLock takes a bounded in-process lock followed by a platform
// cross-process lock. Unix locks the stable user home directory; Windows uses
// a config-path-named mutex. Neither platform lock is an artifact inside
// ConfigDir, so purge can remove that directory without an unlink/share-mode
// race. Process exit releases the platform lock automatically. Every
// preparation/lock error fails closed. NOT reentrant.
func acquireCensusLock(paths runtimePaths) (func(), bool) {
	return acquireCensusLockWithin(paths, censusLockTimeout)
}

func acquireCensusLockWithin(paths runtimePaths, timeout time.Duration) (func(), bool) {
	unlocked := func() {}
	if paths.ConfigDir == "" {
		return unlocked, false
	}
	started := time.Now()
	timer := time.NewTimer(timeout)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	select {
	case censusProcessLock <- struct{}{}:
	case <-timer.C:
		return unlocked, false
	}
	releaseProcess := func() { <-censusProcessLock }
	remaining := timeout - time.Since(started)
	if remaining <= 0 {
		releaseProcess()
		return unlocked, false
	}
	releasePlatform, ok := acquireCensusPlatformLock(paths.ConfigDir, remaining, censusLockRetryInterval)
	if !ok {
		releaseProcess()
		return unlocked, false
	}
	return func() {
		releasePlatform()
		releaseProcess()
	}, true
}

// mutateCensusState is the ONLY read-modify-write path for census writers: it
// holds the census lock around load+mutate+save, and callers mutate only
// their own fields. Together this serializes whole cycles across processes
// (so `census off` always wins) AND keeps concurrent writers from clobbering
// each other's fields. Notice markers are always claimed OUTSIDE this lock.
// The centralized ping path takes this lock directly and does not call this
// helper while holding it.
func mutateCensusState(paths runtimePaths, mutate func(*censusState)) error {
	return mutateCensusStateWithin(paths, censusLockTimeout, mutate)
}

func mutateCensusStateWithin(paths runtimePaths, timeout time.Duration, mutate func(*censusState)) error {
	release, ok := acquireCensusLockWithin(paths, timeout)
	if !ok {
		return fmt.Errorf("census state is locked by another process")
	}
	defer release()
	state, writable := readCensusState(paths)
	if !writable {
		return fmt.Errorf("census state is not writable by this client version")
	}
	before := state
	mutate(&state)
	if state == before {
		return nil
	}
	return saveCensusState(paths, state)
}

func saveCensusState(paths runtimePaths, state censusState) error {
	if paths.CensusFile == "" {
		return fmt.Errorf("census state path unknown")
	}
	if !censusInstallActive(paths) {
		return fmt.Errorf("HA NOVA install is no longer active")
	}
	state.Schema = censusStateSchemaVersion
	return writeJSONFile(paths.CensusFile, state, 0o600)
}

// state.json is the install-lifecycle sentinel. Setup creates it before any
// census entry point; uninstall removes it first while holding the census
// lock. Every census writer checks it under that same lock before saving, so a
// queued old process cannot recreate census.json after uninstall.
func censusInstallActive(paths runtimePaths) bool {
	if paths.StateFile == "" {
		return false
	}
	info, err := os.Stat(paths.StateFile)
	return err == nil && !info.IsDir() && !censusLifecycleStopped(paths)
}

// The uninstall marker lives beside (not inside) a persistent device/user-local
// root, so managed-directory and cache cleanup cannot remove it. It contains only an opaque
// random nonce: no user, consent, timestamp, process, or census data. A setup
// that started after that exact marker was written may
// remove it on successful completion; stale update/setup processes cannot.
func censusLifecycleMarkerPath(paths runtimePaths) string {
	// Cache roots are disposable and cannot uphold an uninstall barrier. Keep
	// the marker beside the managed config root on Unix, and beside the
	// device-local data root on Windows (never roaming APPDATA).
	base := paths.ConfigDir
	if runtime.GOOS == "windows" {
		base = paths.LocalDataDir
	}
	if base == "" {
		if paths.Home == "" {
			return ""
		}
		if runtime.GOOS == "windows" {
			base = filepath.Join(paths.Home, "AppData", "Local", "ha-nova")
		} else {
			base = filepath.Join(paths.Home, ".config", "ha-nova")
		}
	}
	return filepath.Join(filepath.Dir(base), ".ha-nova-census-uninstalled")
}

func censusLifecycleStopped(paths runtimePaths) bool {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return true
	}
	_, err := os.Stat(marker)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func captureCensusLifecycleMarker(paths runtimePaths) []byte {
	data, _ := readCensusLifecycleMarker(paths)
	return data
}

func readCensusLifecycleMarker(paths runtimePaths) ([]byte, error) {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return nil, fmt.Errorf("census lifecycle marker path unknown")
	}
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("census lifecycle marker is empty")
	}
	return data, nil
}

func installLifecycleGenerationPath(paths runtimePaths) string {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(marker), ".ha-nova-install-generation")
}

func captureInstallLifecycleGeneration(paths runtimePaths) []byte {
	data, _ := readInstallLifecycleGeneration(paths)
	return data
}

func readInstallLifecycleGeneration(paths runtimePaths) ([]byte, error) {
	path := installLifecycleGenerationPath(paths)
	if path == "" {
		return nil, fmt.Errorf("install lifecycle generation path unknown")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("install lifecycle generation is empty")
	}
	return data, nil
}

func rotateInstallLifecycleGeneration(paths runtimePaths) error {
	path := installLifecycleGenerationPath(paths)
	if path == "" {
		return fmt.Errorf("install lifecycle generation path unknown")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate install lifecycle nonce: %w", err)
	}
	return writeJSONFile(path, fmt.Sprintf("%x", nonce[:]), 0o600)
}

func markCensusLifecycleStopped(paths runtimePaths) error {
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		return fmt.Errorf("census lifecycle marker path unknown")
	}
	if err := rotateInstallLifecycleGeneration(paths); err != nil {
		return err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate census lifecycle nonce: %w", err)
	}
	value := fmt.Sprintf("%x", nonce[:])
	return writeJSONFile(marker, value, 0o600)
}

func reactivateCensusAfterSetup(paths runtimePaths, captured []byte) (bool, error) {
	if len(captured) == 0 {
		return false, nil
	}
	release, ok := acquireCensusLock(paths)
	if !ok {
		return false, fmt.Errorf("cannot acquire census lifecycle lock")
	}
	defer release()
	marker := censusLifecycleMarkerPath(paths)
	current, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !bytes.Equal(current, captured) {
		return false, nil
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// censusISOWeek renders the UTC ISO-8601 week label, zero-padded so that
// string order equals chronological order ("2026-W05" < "2026-W31").
func censusISOWeek(t time.Time) string {
	year, week := t.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func censusOptedOutByEnv() bool {
	// Raw, untrimmed: the documented contract is "any non-empty value", and a
	// visibly configured HA_NOVA_NO_CENSUS=' ' must suppress too.
	return os.Getenv(censusOptOutEnv) != ""
}

const censusRelayStampInterval = 24 * time.Hour

// censusPreMutateHook is a test seam: it runs between an unlocked consent
// pre-check and the locked mutation, letting tests interleave a concurrent
// opt-out at exactly that point.
var censusPreMutateHook = func() {}

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
	// The env kill switch suppresses ALL census activity, including passive
	// state accrual for an otherwise opted-in install.
	if censusOptedOutByEnv() {
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
	// week stamp written between our load and this save must survive. Re-check
	// consent INSIDE the locked mutation: an opt-out that won the lock between
	// our unlocked pre-check and this write must not be followed by any census
	// state accrual ("only for opted-in installs" holds under races too).
	censusPreMutateHook()
	_ = mutateCensusStateWithin(paths, censusPassiveLockTimeout, func(s *censusState) {
		if !s.Enabled {
			return
		}
		s.RelayVersion = version
		s.RelayVersionObservedAt = now.Format(time.RFC3339)
	})
}
