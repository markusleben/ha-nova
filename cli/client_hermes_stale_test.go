package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A namespaced Hermes bundle that predates a newly added skill must stay
// detectable as configured (so the no-state resync path can repair it) while
// the strict readiness check reports it as not fully present.
func TestStaleNamespacedHermesBundleStaysDetectable(t *testing.T) {
	home := t.TempDir()

	bundleRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
	// Write every required dir EXCEPT the newest skill (scene) — the shape of
	// a pre-scene namespaced install.
	for _, skillDir := range hermesRequiredSkillDirs {
		if skillDir == "ha-nova-scene" {
			continue
		}
		dir := filepath.Join(bundleRoot, skillDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+skillDir+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", skillDir, err)
		}
	}

	if hermesBundlePresent(home) {
		t.Fatal("hermesBundlePresent() = true for a pre-scene bundle, want false (stale bundle must trigger resync)")
	}
	if !hermesNamespacedContextPresent(home) {
		t.Fatal("hermesNamespacedContextPresent() = false, want true (stale namespaced bundle must stay detectable)")
	}
	if hermesLegacyBundlePresent(home) {
		t.Fatal("hermesLegacyBundlePresent() = true for a namespaced bundle, want false")
	}
}
