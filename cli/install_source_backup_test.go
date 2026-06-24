package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mockInstallSourceExe points executablePathForInstallSource at a fixed path for
// the duration of the test (restored on cleanup). This simulates os.Executable()
// resolving the running binary — including the in-place-update case where Linux
// follows /proc/self/exe into the renamed `.ha-nova-old-*` backup.
func mockInstallSourceExe(t *testing.T, path string) {
	t.Helper()
	orig := executablePathForInstallSource
	executablePathForInstallSource = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePathForInstallSource = orig })
}

func writeBundleTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeMinimalBundleTree builds a self-contained, platform-matched bundle root
// good enough for validateBundleRoot and the file-client adapters: bundle.json,
// a fake runtime binary, version.json, a 3-client registry, a context skill that
// references a shared doc (so the Hermes/Antigravity adapters bake an absolute
// sourceRoot path we can assert on), and one sub-skill. includeWriteSafety adds
// the canonical skills/ha-nova/write-safety.md — present in the NEW bundle and
// absent in the stale backup, exactly mirroring the user's bug report.
func writeMinimalBundleTree(t *testing.T, root, version string, includeWriteSafety bool) {
	t.Helper()
	meta := fmt.Sprintf(`{"bundle_format_version":1,"os":%q,"arch":%q,"binary_name":%q,"version":%q}`,
		bundlePlatformOS(), bundlePlatformArch(), publicBinaryName(), version)
	writeBundleTestFile(t, filepath.Join(root, "bundle.json"), meta, 0o644)
	writeBundleTestFile(t, filepath.Join(root, publicBinaryName()), "runtime "+version, 0o755)
	writeBundleTestFile(t, filepath.Join(root, "version.json"),
		fmt.Sprintf(`{"skill_version":%q,"min_relay_version":"0.1.0"}`, version), 0o644)
	writeBundleTestFile(t, filepath.Join(root, "clients", "registry.json"),
		`{"clients":[`+
			`{"id":"hermes","label":"Hermes Agent","adapter_kind":"skill_tree","supported_os":["macos","linux"]},`+
			`{"id":"codex","label":"Codex CLI","adapter_kind":"skill_tree","supported_os":["macos","linux","windows"]},`+
			`{"id":"antigravity","label":"Google Antigravity CLI","adapter_kind":"skill_flat","supported_os":["macos","linux","windows"]}`+
			`]}`, 0o644)
	writeBundleTestFile(t, filepath.Join(root, "docs", "reference", "foo.md"), "# Foo\n", 0o644)
	ctx := "---\nname: ha-nova\n---\n\nContext skill. See `docs/reference/foo.md` for details.\n"
	writeBundleTestFile(t, filepath.Join(root, "skills", "ha-nova", "SKILL.md"), ctx, 0o644)
	if includeWriteSafety {
		writeBundleTestFile(t, filepath.Join(root, "skills", "ha-nova", "write-safety.md"), "# Write Safety\n", 0o644)
	}
	writeBundleTestFile(t, filepath.Join(root, "skills", "read", "SKILL.md"), "---\nname: read\n---\n\nRead skill.\n", 0o644)
}

func TestResolveSourceRootSkipsTransientBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// Fresh bundle lives at the install root after the swap.
	writeBundleTestFile(t, filepath.Join(paths.InstallRoot, "bundle.json"), "{}", 0o644)
	// The stale backup sibling still carries a bundle.json — indistinguishable from
	// the install root except by its transient-backup basename.
	backup := filepath.Join(filepath.Dir(paths.InstallRoot), installBackupPrefixOld+"123")
	writeBundleTestFile(t, filepath.Join(backup, "bundle.json"), "{}", 0o644)

	// On Linux the running binary was renamed INTO the backup during the swap.
	mockInstallSourceExe(t, filepath.Join(backup, publicBinaryName()))

	got := resolveSourceRoot(paths)
	if filepath.Clean(got) != filepath.Clean(paths.InstallRoot) {
		t.Fatalf("resolveSourceRoot() = %q, want install root %q (must never resolve the transient backup %q)",
			got, paths.InstallRoot, backup)
	}
}

func TestResolveSourceRootKeepsPortableInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// A legitimate portable install: the exe sits in a stable custom dir that holds
	// the bundle, while the default install root has none. This must still resolve
	// to the portable dir (the fix only excludes transient-backup basenames).
	portable := filepath.Join(t.TempDir(), "portable-ha-nova")
	writeBundleTestFile(t, filepath.Join(portable, "bundle.json"), "{}", 0o644)
	mockInstallSourceExe(t, filepath.Join(portable, publicBinaryName()))

	got := resolveSourceRoot(paths)
	if filepath.Clean(got) != filepath.Clean(portable) {
		t.Fatalf("resolveSourceRoot() = %q, want portable dir %q (no regression for portable installs)", got, portable)
	}
}

func TestSourceRootCandidatesExcludeTransientBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	backup := filepath.Join(filepath.Dir(paths.InstallRoot), installBackupPrefixOld+"456")
	mockInstallSourceExe(t, filepath.Join(backup, publicBinaryName()))

	candidates := sourceRootCandidates(paths)
	for _, candidate := range candidates {
		if filepath.Clean(candidate) == filepath.Clean(backup) {
			t.Fatalf("sourceRootCandidates() leaked the transient backup %q: %v", backup, candidates)
		}
	}
	foundInstallRoot := false
	for _, candidate := range candidates {
		if filepath.Clean(candidate) == filepath.Clean(paths.InstallRoot) {
			foundInstallRoot = true
			break
		}
	}
	if !foundInstallRoot {
		t.Fatalf("sourceRootCandidates() missing install root %q: %v", paths.InstallRoot, candidates)
	}
}

// TestPostUpdateSyncAfterRealSwapSyncsFromInstallRootNotBackup is the end-to-end
// proof: it drives the ACTUAL rename swap (applyStagedBundleWithRollback), points
// the running binary into the resulting `.ha-nova-old-*` backup (the Linux
// rename-follow), then runs postUpdateSync. With the bug, clients would sync from
// the stale backup (no write-safety.md, SKILL.md paths baked into `.ha-nova-old-`)
// which commit() then deletes. With the fix they sync from the fresh install root.
func TestPostUpdateSyncAfterRealSwapSyncsFromInstallRootNotBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("in-place update runs out-of-process on Windows; the backup rename-follow bug is non-Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	originalRuntimeDetected := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return true }
	t.Cleanup(func() { clientRuntimeDetectedForStatus = originalRuntimeDetected })

	// Stale OLD install root (no write-safety.md) — becomes the backup after the swap.
	writeMinimalBundleTree(t, paths.InstallRoot, "0.6.0", false)
	if err := os.MkdirAll(filepath.Dir(paths.PublicBinary), 0o755); err != nil {
		t.Fatalf("mkdir public binary dir: %v", err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("old-link"), 0o755); err != nil {
		t.Fatalf("write public binary: %v", err)
	}

	// Fresh NEW bundle (with write-safety.md) staged for the swap.
	stageRoot := filepath.Join(t.TempDir(), "stage", "ha-nova")
	writeMinimalBundleTree(t, stageRoot, "0.6.1", true)

	// Configure Hermes + Codex so postUpdateSync re-syncs them.
	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.0",
		InstalledClients: []string{"hermes", "codex"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	_, commitInstall, err := applyStagedBundleWithRollback(paths, stageRoot)
	if err != nil {
		t.Fatalf("applyStagedBundleWithRollback() error: %v", err)
	}

	// Locate the backup the swap created and point the running binary into it.
	backup := findTransientBackup(t, filepath.Dir(paths.InstallRoot))
	mockInstallSourceExe(t, filepath.Join(backup, publicBinaryName()))

	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}
	// Mirror the real flow: commit() deletes the backup. A bug-synced client would
	// now hold dangling references into a deleted tree.
	if err := commitInstall(); err != nil {
		t.Fatalf("commit() error: %v", err)
	}

	// Hermes context skill must carry the NEW write-safety.md (synced from the
	// install root, not the stale backup).
	writeSafety := filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova", "write-safety.md")
	if _, err := os.Stat(writeSafety); err != nil {
		t.Fatalf("expected Hermes write-safety.md synced from the fresh install root, got: %v", err)
	}

	// No installed SKILL.md may bake a transient-backup path.
	assertNoBackupRefsUnder(t, filepath.Join(home, ".hermes", "skills", "ha-nova"))

	// The Codex symlink must resolve under the install root, not the deleted backup.
	codexLink := filepath.Join(home, ".agents", "skills", "ha-nova")
	resolved, err := filepath.EvalSymlinks(codexLink)
	if err != nil {
		t.Fatalf("Codex skill tree did not resolve (dangling backup symlink?): %v", err)
	}
	installRootResolved, err := filepath.EvalSymlinks(paths.InstallRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(InstallRoot) error: %v", err)
	}
	if !strings.HasPrefix(resolved, installRootResolved) {
		t.Fatalf("Codex skill tree resolved to %q, want a path under the install root %q", resolved, installRootResolved)
	}

	// A clean post-swap sync (no skip/fail and no transient-backup residue) must
	// stamp the verification marker, proving the residue scan does not false-flag a
	// correctly synced tree.
	healed, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if healed.ClientsVerifiedVersion != "0.6.1" {
		t.Fatalf("clean post-swap sync must stamp the marker to 0.6.1, got %q", healed.ClientsVerifiedVersion)
	}
}

func findTransientBackup(t *testing.T, parent string) string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read parent %s: %v", parent, err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), installBackupPrefixOld) {
			return filepath.Join(parent, entry.Name())
		}
	}
	t.Fatalf("no %s* backup found in %s after swap", installBackupPrefixOld, parent)
	return ""
}

func assertNoBackupRefsUnder(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, prefix := range []string{installBackupPrefixOld, installBackupPrefixNext, installBackupPrefixFailed} {
			if strings.Contains(string(data), prefix) {
				t.Fatalf("installed skill %s references a transient backup (%s):\n%s", path, prefix, string(data))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
