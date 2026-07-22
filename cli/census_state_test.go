package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCensusISOWeekLabels(t *testing.T) {
	cases := []struct {
		name string
		time time.Time
		want string
	}{
		// 2026-01-01 is a Thursday — ISO week 1 of 2026.
		{"first january", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), "2026-W01"},
		// 2027-01-01 is a Friday — it still belongs to ISO week 53 of 2026.
		{"iso year differs from calendar year", time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC), "2026-W53"},
		// Zero padding keeps string order chronological.
		{"single digit week is padded", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "2026-W06"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := censusISOWeek(tc.time); got != tc.want {
				t.Fatalf("censusISOWeek(%s) = %q, want %q", tc.time, got, tc.want)
			}
		})
	}
	// The label is computed in UTC regardless of the local zone of the input.
	zoned := time.Date(2026, 1, 5, 1, 0, 0, 0, time.FixedZone("east", 3*3600))
	if got, want := censusISOWeek(zoned), censusISOWeek(zoned.UTC()); got != want {
		t.Fatalf("zoned label %q != UTC label %q", got, want)
	}
}

func TestCensusStateRoundTripAndCorruptFileDefaults(t *testing.T) {
	paths := setupCensusTest(t)
	state := censusState{
		AskedAt:      "2026-07-22T10:00:00Z",
		AskedVia:     "setup",
		Answer:       "yes",
		Enabled:      true,
		LastPingWeek: "2026-W30",
		SkillNotices: 2,
	}
	if err := saveCensusState(paths, state); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	loaded := loadCensusState(paths)
	if loaded.Schema != censusStateSchemaVersion || loaded.AskedVia != "setup" || !loaded.Enabled || loaded.LastPingWeek != "2026-W30" || loaded.SkillNotices != 2 {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}

	corrupt := []byte("{not json")
	if err := os.WriteFile(paths.CensusFile, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	got := loadCensusState(paths)
	if got.Enabled || got.AskedAt == "" || got.Answer != "none" || got.AskedVia != "recovered" {
		t.Fatalf("corrupt census.json must recover to a stamped disabled default (no re-ask nag, no re-enable), got %+v", got)
	}
	if raw, err := os.ReadFile(paths.CensusFile); err != nil || !bytes.Equal(raw, corrupt) {
		t.Fatalf("read-only recovery changed corrupt census.json: bytes=%q err=%v", raw, err)
	}
	// Repeated reads stay safe without turning an unlocked read into a writer.
	reloaded := loadCensusState(paths)
	if reloaded.AskedAt == "" || reloaded.Enabled || reloaded.Answer != "none" {
		t.Fatalf("repeated corrupt-state recovery must stay safely disabled, got %+v", reloaded)
	}
	if raw, err := os.ReadFile(paths.CensusFile); err != nil || !bytes.Equal(raw, corrupt) {
		t.Fatalf("repeated recovery changed corrupt census.json: bytes=%q err=%v", raw, err)
	}
}

func TestCensusStateFutureSchemaRecoversToStampedDisabledDefault(t *testing.T) {
	paths := setupCensusTest(t)
	if err := os.MkdirAll(filepath.Dir(paths.CensusFile), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	future := []byte(`{"schema":99,"enabled":true,"answer":"yes"}`)
	if err := os.WriteFile(paths.CensusFile, future, 0o600); err != nil {
		t.Fatalf("write future-schema file: %v", err)
	}
	got := loadCensusState(paths)
	if got.Enabled || got.AskedAt == "" || got.Answer != "none" {
		t.Fatalf("a future schema must never silently re-enable or re-ask, got %+v", got)
	}
	if got.Schema != censusStateSchemaVersion {
		t.Fatalf("recovered schema = %d, want %d", got.Schema, censusStateSchemaVersion)
	}
	if raw, err := os.ReadFile(paths.CensusFile); err != nil || !bytes.Equal(raw, future) {
		t.Fatalf("older read must preserve future-schema census.json byte-for-byte: bytes=%q err=%v", raw, err)
	}
}

func TestCensusMutationPreservesFutureAndCorruptBytes(t *testing.T) {
	paths := setupCensusTest(t)
	for name, original := range map[string][]byte{
		"future":  []byte(`{"schema":99,"enabled":true,"answer":"yes"}`),
		"corrupt": []byte(`{not-json`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(paths.CensusFile, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := mutateCensusState(paths, func(*censusState) {}); err == nil {
				t.Fatal("mutation unexpectedly accepted non-writable census state")
			}
			got, err := os.ReadFile(paths.CensusFile)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("mutation changed protected bytes: got=%q err=%v", got, err)
			}
		})
	}
}

func TestCensusLifecycleMarkerUsesSetupStartCAS(t *testing.T) {
	paths := setupCensusTest(t)
	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatal(err)
	}
	captured := captureCensusLifecycleMarker(paths)
	if len(captured) == 0 {
		t.Fatal("expected lifecycle marker snapshot")
	}
	if err := markCensusLifecycleStopped(paths); err != nil {
		t.Fatal(err)
	}
	if reactivated, err := reactivateCensusAfterSetup(paths, captured); err != nil || reactivated {
		t.Fatalf("stale setup reactivation = %v, err=%v", reactivated, err)
	}
	current := captureCensusLifecycleMarker(paths)
	if len(current) == 0 || bytes.Equal(current, captured) {
		t.Fatal("newer uninstall marker was not preserved")
	}
	if reactivated, err := reactivateCensusAfterSetup(paths, current); err != nil || !reactivated {
		t.Fatalf("fresh setup reactivation = %v, err=%v", reactivated, err)
	}
	if censusLifecycleStopped(paths) {
		t.Fatal("matching successful setup did not clear lifecycle stop")
	}
}

func TestCensusLifecycleMarkerIsOutsideDisposableCache(t *testing.T) {
	paths := setupCensusTest(t)
	marker := censusLifecycleMarkerPath(paths)
	if marker == "" {
		t.Fatal("missing lifecycle marker path")
	}
	if strings.HasPrefix(marker, paths.CacheDir+string(os.PathSeparator)) || marker == paths.CacheDir {
		t.Fatalf("lifecycle marker must not live in disposable cache: %s", marker)
	}
	if runtime.GOOS != "windows" && filepath.Dir(marker) != filepath.Dir(paths.ConfigDir) {
		t.Fatalf("Unix marker parent = %s, want persistent config parent %s", filepath.Dir(marker), filepath.Dir(paths.ConfigDir))
	}
}

func TestMutateCensusStatePreservesOtherWritersFields(t *testing.T) {
	paths := setupCensusTest(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	// Two writers with disjoint fields, each based on a reload: both survive.
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = "2026-W30" }); err != nil {
		t.Fatalf("mutate week: %v", err)
	}
	if err := mutateCensusState(paths, func(s *censusState) { s.RelayVersion = "0.9.0" }); err != nil {
		t.Fatalf("mutate relay: %v", err)
	}
	got := loadCensusState(paths)
	if got.LastPingWeek != "2026-W30" || got.RelayVersion != "0.9.0" || !got.Enabled {
		t.Fatalf("disjoint mutations must not clobber each other: %+v", got)
	}
}

func TestStampCensusRelayVersionPreservesFreshWeekStamp(t *testing.T) {
	paths := setupCensusTest(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	// A carrier stamps the week; the relay observer writes afterwards and must
	// keep it (it goes through the reload-mutate path touching only its fields).
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = "2026-W30" }); err != nil {
		t.Fatalf("mutate week: %v", err)
	}
	stampCensusRelayVersion(paths, "0.9.0")
	got := loadCensusState(paths)
	if got.LastPingWeek != "2026-W30" {
		t.Fatalf("relay stamp clobbered the week: %+v", got)
	}
	if got.RelayVersion != "0.9.0" {
		t.Fatalf("relay stamp missing: %+v", got)
	}
}

// The P1 contract: an explicit opt-out must ALWAYS win, no matter which
// automatic mutators (week stamps, relay observations, notice counters) run
// concurrently — the census lock serializes every load+mutate+save cycle.
func TestCensusOffAlwaysWinsAgainstConcurrentMutators(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes", AskedAt: "2026-07-01T00:00:00Z"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_ = mutateCensusState(paths, func(s *censusState) {
					s.LastPingWeek = fmt.Sprintf("2026-W%02d", (n*5+j)%50+1)
				})
				_ = mutateCensusState(paths, func(s *censusState) {
					s.RelayVersion = fmt.Sprintf("0.%d.%d", n, j)
					s.RelayVersionObservedAt = "2026-07-01T00:00:00Z"
				})
			}
		}(i)
	}
	exit := 0
	captureStdout(t, func() { exit = runCensusCommand(paths, []string{"off"}) })
	wg.Wait()

	if exit != 0 {
		t.Fatalf("census off exit = %d, want 0", exit)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "no" {
		t.Fatalf("census off must survive every concurrent mutator: %+v", state)
	}
}

func TestCensusCoordinatorSerializesAndReleases(t *testing.T) {
	paths := setupCensusTest(t)
	originalRetry, originalTimeout := censusLockRetryInterval, censusLockTimeout
	censusLockRetryInterval = time.Millisecond
	censusLockTimeout = 30 * time.Millisecond
	t.Cleanup(func() {
		censusLockRetryInterval, censusLockTimeout = originalRetry, originalTimeout
	})

	release, ok := acquireCensusLock(paths)
	if !ok {
		t.Fatal("first caller must acquire a fresh census lock")
	}
	// A second caller in this process must hit the bounded process-local layer;
	// the platform layer provides the same exclusion across processes.
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = "2026-W30" }); err == nil {
		t.Fatal("a held lock must make the mutation fail, not proceed")
	}
	release()
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = "2026-W30" }); err != nil {
		t.Fatalf("released census lock must be acquirable again, got %v", err)
	}
	if state := loadCensusState(paths); state.LastPingWeek != "2026-W30" {
		t.Fatalf("mutation after release missing: %+v", state)
	}
}

func TestCensusCoordinatorSerializesAcrossProcesses(t *testing.T) {
	const helperEnv = "HA_NOVA_TEST_CENSUS_LOCK_HELPER"
	if os.Getenv(helperEnv) == "1" {
		paths := runtimePaths{ConfigDir: os.Getenv("HA_NOVA_TEST_CENSUS_CONFIG_DIR")}
		release, ok := acquireCensusLock(paths)
		if !ok {
			t.Fatal("helper could not acquire census lock")
		}
		defer release()
		if err := os.WriteFile(os.Getenv("HA_NOVA_TEST_CENSUS_LOCK_SIGNAL"), []byte("locked"), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		return
	}

	paths := setupCensusTest(t)
	signal := filepath.Join(t.TempDir(), "locked")
	command := exec.Command(os.Args[0], "-test.run=^TestCensusCoordinatorSerializesAcrossProcesses$")
	command.Env = append(os.Environ(),
		helperEnv+"=1",
		"HA_NOVA_TEST_CENSUS_CONFIG_DIR="+paths.ConfigDir,
		"HA_NOVA_TEST_CENSUS_LOCK_SIGNAL="+signal,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(signal); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatal("helper did not signal census-lock ownership")
		}
		time.Sleep(10 * time.Millisecond)
	}

	originalRetry, originalTimeout := censusLockRetryInterval, censusLockTimeout
	censusLockRetryInterval = time.Millisecond
	censusLockTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		censusLockRetryInterval, censusLockTimeout = originalRetry, originalTimeout
	})
	if release, ok := acquireCensusLock(paths); ok {
		release()
		t.Fatal("parent acquired the cross-process census lock while helper held it")
	}
	censusLockRetryInterval, censusLockTimeout = originalRetry, originalTimeout
	if err := command.Wait(); err != nil {
		t.Fatalf("census-lock helper failed: %v", err)
	}
	release, ok := acquireCensusLock(paths)
	if !ok {
		t.Fatal("cross-process census lock was not released after helper exit")
	}
	release()
}

func TestStampCensusRelayVersionOnlyWhenEnabled(t *testing.T) {
	paths := setupCensusTest(t)

	stampCensusRelayVersion(paths, "0.7.0")
	if _, err := os.Stat(paths.CensusFile); !os.IsNotExist(err) {
		t.Fatalf("stamp without opt-in must not create census.json (err=%v)", err)
	}

	if err := saveCensusState(paths, censusState{Enabled: false, Answer: "no"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	stampCensusRelayVersion(paths, "0.7.0")
	if state := loadCensusState(paths); state.RelayVersion != "" {
		t.Fatalf("stamp must be a no-op for disabled installs, got %q", state.RelayVersion)
	}
}

func TestStampCensusRelayVersionRechecksConsentInsideTheLock(t *testing.T) {
	// An opt-out that wins the lock between the stamp's unlocked pre-check and
	// its locked write must not be followed by relay-field accrual: the locked
	// mutation re-checks Enabled, so nothing census-related is recorded for an
	// install that just revoked consent.
	paths := setupCensusTest(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	prev := censusPreMutateHook
	censusPreMutateHook = func() {
		// Simulates `census off` winning the lock first: the mutation below
		// reloads and sees the revoked state.
		censusPreMutateHook = prev
		if err := mutateCensusState(paths, func(s *censusState) {
			s.Enabled = false
			s.Answer = "no"
		}); err != nil {
			t.Fatalf("concurrent opt-out: %v", err)
		}
	}
	t.Cleanup(func() { censusPreMutateHook = prev })

	stampCensusRelayVersion(paths, "0.7.0")
	state := loadCensusState(paths)
	if state.Enabled {
		t.Fatal("opt-out must win")
	}
	if state.RelayVersion != "" || state.RelayVersionObservedAt != "" {
		t.Fatalf("relay fields must not accrue after consent was revoked, got %q/%q", state.RelayVersion, state.RelayVersionObservedAt)
	}
}

func TestStampCensusRelayVersionThrottlesWrites(t *testing.T) {
	paths := setupCensusTest(t)
	base := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	originalNow := censusNow
	t.Cleanup(func() { censusNow = originalNow })
	censusNow = func() time.Time { return base }

	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	stampCensusRelayVersion(paths, "0.7.0")
	first := loadCensusState(paths)
	if first.RelayVersion != "0.7.0" || first.RelayVersionObservedAt == "" {
		t.Fatalf("first observation must stamp, got %+v", first)
	}

	// Same value one hour later: throttled, no rewrite.
	censusNow = func() time.Time { return base.Add(time.Hour) }
	stampCensusRelayVersion(paths, "0.7.0")
	if got := loadCensusState(paths); got.RelayVersionObservedAt != first.RelayVersionObservedAt {
		t.Fatal("unchanged value within 24h must not rewrite the stamp")
	}

	// Changed value: immediate rewrite.
	stampCensusRelayVersion(paths, "0.8.0")
	changed := loadCensusState(paths)
	if changed.RelayVersion != "0.8.0" {
		t.Fatalf("changed value must stamp immediately, got %q", changed.RelayVersion)
	}

	// Same value again but beyond 24h: refresh the observation time.
	censusNow = func() time.Time { return base.Add(26 * time.Hour) }
	stampCensusRelayVersion(paths, "0.8.0")
	refreshed := loadCensusState(paths)
	if refreshed.RelayVersionObservedAt == changed.RelayVersionObservedAt {
		t.Fatal("a stamp older than 24h must be refreshed")
	}
}

func TestUninstallCacheCleanupIncludesCensusMarkers(t *testing.T) {
	// Legacy census-ping and census-notice markers must not survive uninstall
	// into a same-HOME reinstall. Current serialization creates no markers.
	cacheDir := t.TempDir()
	for _, name := range []string{"census-ping-2026-W30", "census-notice-1", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := runtimePaths{CacheDir: cacheDir, UpdateCacheFile: filepath.Join(cacheDir, "latest-release.json")}
	list := managedCacheArtifactPaths(paths)
	for _, want := range []string{"census-ping-2026-W30", "census-notice-1"} {
		found := false
		for _, p := range list {
			if p == filepath.Join(cacheDir, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("cache cleanup list must include %s, got %v", want, list)
		}
	}
	for _, p := range list {
		if p == filepath.Join(cacheDir, "unrelated.txt") {
			t.Fatal("cache cleanup must not sweep unrelated files")
		}
	}
}

func TestStampCensusRelayVersionHonorsEnvKillSwitch(t *testing.T) {
	// HA_NOVA_NO_CENSUS suppresses ALL census activity — including passive
	// relay-version accrual for an otherwise opted-in install.
	paths := setupCensusTest(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	t.Setenv(censusOptOutEnv, "1")
	stampCensusRelayVersion(paths, "0.7.0")
	if state := loadCensusState(paths); state.RelayVersion != "" {
		t.Fatalf("relay stamp must be suppressed under the kill switch, got %q", state.RelayVersion)
	}
}

func TestCensusEnvKillSwitchHonorsWhitespaceValue(t *testing.T) {
	// The documented contract is "any non-empty value" — a visibly configured
	// HA_NOVA_NO_CENSUS=' ' must suppress too (raw check, no trimming).
	t.Setenv(censusOptOutEnv, " ")
	if !censusOptedOutByEnv() {
		t.Fatal("whitespace-only value must count as set")
	}
}
