package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	censusStateSchemaVersion = 3
	censusConsentVersion     = 2
	censusInstallationPrefix = "cns-"
	censusChoicePrefix       = "cns-choice-"
)

// censusNow is the census clock, overridable for cadence, relay-freshness,
// retention, and throttle tests.
var censusNow = time.Now

// censusState is per-device and profile-independent (under LOCALAPPDATA on
// Windows; next to config.json elsewhere), and removed by uninstall. The
// dedicated Census installation id is never reused for pairing or Relay auth.
type censusState struct {
	Schema             int    `json:"schema"`
	ConsentVersion     int    `json:"consent_version,omitempty"`
	InstallationID     string `json:"installation_id,omitempty"`
	AskedAt            string `json:"asked_at,omitempty"`
	AskedVia           string `json:"asked_via,omitempty"`
	Answer             string `json:"answer,omitempty"` // yes | no | none
	Enabled            bool   `json:"enabled"`
	LastAttemptAt      string `json:"last_attempt_at,omitempty"`
	WithdrawalPending  bool   `json:"withdrawal_pending,omitempty"`
	PendingChoiceID    string `json:"pending_choice_id,omitempty"`
	LastPingWeek       string `json:"last_ping_week,omitempty"`      // Schema 1/2 migration only.
	SkillNotices       int    `json:"skill_notices,omitempty"`       // Legacy schema-1 machine emissions; never counts as a visible choice.
	SkillPresentations int    `json:"skill_presentations,omitempty"` // Confirmed visible choices under the current contract.
	// Relay version stamped from normal relay traffic (checkRelayVersionValue
	// funnel) — the census NEVER makes its own relay call.
	RelayVersion           string `json:"relay_version,omitempty"`
	RelayVersionObservedAt string `json:"relay_version_observed_at,omitempty"`
}

func defaultCensusState() censusState {
	return censusState{Schema: censusStateSchemaVersion}
}

func newCensusInstallationID() (string, error) {
	return newCensusRandomID(censusInstallationPrefix)
}

func newCensusChoiceID() (string, error) {
	return newCensusRandomID(censusChoicePrefix)
}

func newCensusRandomID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate census random id: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func ensureCensusInstallationID(paths runtimePaths) (string, error) {
	candidate, err := newCensusInstallationID()
	if err != nil {
		return "", err
	}
	id := ""
	if err := mutateCensusState(paths, func(state *censusState) {
		if !censusInstallationIDPattern.MatchString(state.InstallationID) {
			state.InstallationID = candidate
		}
		id = state.InstallationID
	}); err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("census installation id is empty")
	}
	return id, nil
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
	if state.Schema < 3 {
		migrateLegacyCensusConsent(&state)
		return state, true
	}
	return state, true
}

// Schema 1/2 consent explicitly promised an identifier-free payload. A prior
// Yes therefore cannot authorize schema 2 on the wire. Explicit No remains
// final; every other old state reopens the choice without sending.
func migrateLegacyCensusConsent(state *censusState) {
	previousAnswer := state.Answer
	state.Schema = censusStateSchemaVersion
	state.Enabled = false
	state.LastPingWeek = ""
	state.LastAttemptAt = ""
	state.WithdrawalPending = false
	state.PendingChoiceID = ""
	state.InstallationID = ""
	state.SkillNotices = 0
	if previousAnswer == "no" {
		state.ConsentVersion = censusConsentVersion
		state.Answer = "no"
		return
	}
	state.ConsentVersion = 0
	state.AskedAt = ""
	state.AskedVia = ""
	state.Answer = ""
	state.SkillPresentations = 0
}

// recoverCensusState returns a stamped, disabled default without writing. That
// prevents an unlocked status/ask read from overwriting a concurrent consent
// or cadence-stamp mutation. The non-empty ask stamp also prevents a corrupt or
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
	if !state.Enabled || state.ConsentVersion != censusConsentVersion {
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
	// cadence stamp written between our load and this save must survive. Re-check
	// consent INSIDE the locked mutation: an opt-out that won the lock between
	// our unlocked pre-check and this write must not be followed by any census
	// state accrual ("only for opted-in installs" holds under races too).
	censusPreMutateHook()
	_ = mutateCensusStateWithin(paths, censusPassiveLockTimeout, func(s *censusState) {
		if !s.Enabled || s.ConsentVersion != censusConsentVersion {
			return
		}
		s.RelayVersion = version
		s.RelayVersionObservedAt = now.Format(time.RFC3339)
	})
}
