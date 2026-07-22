package main

import (
	"os"
	"path/filepath"
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

	if err := os.WriteFile(paths.CensusFile, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	got := loadCensusState(paths)
	if got.Enabled || got.AskedAt == "" || got.Answer != "none" || got.AskedVia != "recovered" {
		t.Fatalf("corrupt census.json must recover to a stamped disabled default (no re-ask nag, no re-enable), got %+v", got)
	}
	// The recovery is persisted: the next load sees a valid, already-asked file.
	reloaded := loadCensusState(paths)
	if reloaded.AskedAt != got.AskedAt || reloaded.Enabled {
		t.Fatalf("recovered state must be persisted, got %+v", reloaded)
	}
}

func TestCensusStateFutureSchemaRecoversToStampedDisabledDefault(t *testing.T) {
	paths := setupCensusTest(t)
	if err := os.MkdirAll(filepath.Dir(paths.CensusFile), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(paths.CensusFile, []byte(`{"schema":99,"enabled":true,"answer":"yes"}`), 0o600); err != nil {
		t.Fatalf("write future-schema file: %v", err)
	}
	got := loadCensusState(paths)
	if got.Enabled || got.AskedAt == "" || got.Answer != "none" {
		t.Fatalf("a future schema must never silently re-enable or re-ask, got %+v", got)
	}
	if got.Schema != censusStateSchemaVersion {
		t.Fatalf("recovered schema = %d, want %d", got.Schema, censusStateSchemaVersion)
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
