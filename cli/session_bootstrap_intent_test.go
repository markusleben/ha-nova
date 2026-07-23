package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOlderCarrierIntentMigrationIsExact(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name    string
		value   string
		migrate bool
		active  bool
	}{
		{"old pending", "0.6.0" + sessionBootstrapCarrierPendingSuffix, true, false},
		{"fresh old running", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Unix()), false, true},
		{"stale old running", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Add(-sessionBootstrapCarrierClaimTTL-time.Minute).Unix()), true, false},
		{"future old running", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Add(time.Hour).Unix()), true, false},
		{"malformed old running", "0.6.0" + sessionBootstrapCarrierRunningPrefix + "bad", true, false},
		{"signed old running", "0.6.0" + sessionBootstrapCarrierRunningPrefix + "+1", true, false},
		{"zero-padded old running", "0.6.0" + sessionBootstrapCarrierRunningPrefix + "01", true, false},
		{"garbage pending", "garbage" + sessionBootstrapCarrierPendingSuffix, false, false},
		{"leading v pending", "v0.6.0" + sessionBootstrapCarrierPendingSuffix, false, false},
		{"leading space pending", " 0.6.0" + sessionBootstrapCarrierPendingSuffix, false, false},
		{"embedded marker", "prefix:carrier-pending:suffix", false, false},
		{"extra running suffix", "0.6.0" + sessionBootstrapCarrierRunningPrefix + "1:extra", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "marker")
			if err := os.WriteFile(path, []byte(tc.value+"\n"), 0o600); err != nil {
				t.Fatalf("write marker: %v", err)
			}
			migrate, active := olderCarrierIntentNeedsMigration(path, "0.6.1", now)
			if migrate != tc.migrate || active != tc.active {
				t.Fatalf("got migrate=%v active=%v, want %v/%v", migrate, active, tc.migrate, tc.active)
			}
		})
	}
}

func TestSessionBootstrapRepairRespectsCrossVersionCarrierState(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name          string
		value         string
		wantCandidate bool
		wantPending   bool
		wantUnchanged bool
	}{
		{"fresh running stays owned by old process", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Unix()), false, false, true},
		{"stale running is reclaimed", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Add(-sessionBootstrapCarrierClaimTTL-time.Minute).Unix()), true, true, false},
		{"future running is reclaimed", fmt.Sprintf("0.6.0%s%d", sessionBootstrapCarrierRunningPrefix, now.Add(time.Hour).Unix()), true, true, false},
		{"malformed running is reclaimed", "0.6.0" + sessionBootstrapCarrierRunningPrefix + "bad", true, true, false},
		{"garbage marker falls through to layout verification", "garbage:carrier-pending", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupHealableInstall(t)
			codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
				t.Fatalf("seed current Codex tree: %v", err)
			}
			if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
				t.Fatalf("mkdir cache: %v", err)
			}
			marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
			if err := os.WriteFile(marker, []byte(tc.value+"\n"), 0o600); err != nil {
				t.Fatalf("write marker: %v", err)
			}
			if got := repairMissingSessionBootstrap(paths); got != tc.wantCandidate {
				t.Fatalf("candidate = %v, want %v", got, tc.wantCandidate)
			}
			if tc.wantPending && !markerHasCarrierPending(marker, "0.6.1") {
				t.Fatal("reclaimed intent did not become current pending")
			}
			if tc.wantUnchanged {
				data, err := os.ReadFile(marker)
				if err != nil || string(data) != tc.value+"\n" {
					t.Fatalf("fresh old claim changed: %q %v", data, err)
				}
			}
			if !tc.wantPending && !tc.wantUnchanged && !markerHasVersion(marker, "0.6.1") {
				t.Fatal("noncanonical marker did not fall through to verified layout")
			}
		})
	}
}

func TestGenericVerifierPreservesFreshOlderCarrierOwner(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	value := fmt.Sprintf("0.6.0%s%d\n", sessionBootstrapCarrierRunningPrefix, time.Now().Unix())
	if err := os.WriteFile(marker, []byte(value), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	markSessionBootstrapLayoutVerified(paths)

	data, err := os.ReadFile(marker)
	if err != nil || string(data) != value {
		t.Fatalf("generic verifier changed old live owner: %q %v", data, err)
	}
}
