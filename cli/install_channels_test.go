package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWingetPackageStatusParsesInstalledAndAvailableVersions(t *testing.T) {
	output := `
Name      Id                    Version  Available  Source
----------------------------------------------------------
HA NOVA   markusleben.ha-nova   0.3.0    0.4.0      winget
`

	status := parseWingetPackageStatus(output)
	if !status.Installed {
		t.Fatal("expected package to be detected as installed")
	}
	if !status.UpgradeAvailable {
		t.Fatal("expected package to be detected as upgrade-available")
	}
	if status.InstalledVersion != "0.3.0" {
		t.Fatalf("InstalledVersion = %q, want 0.3.0", status.InstalledVersion)
	}
	if status.AvailableVersion != "0.4.0" {
		t.Fatalf("AvailableVersion = %q, want 0.4.0", status.AvailableVersion)
	}
}

func TestIsWingetNoApplicationsFoundExitCodeHandlesSignedAndUnsignedRepresentations(t *testing.T) {
	if !isWingetNoApplicationsFoundExitCode(int(wingetNoApplicationsFoundExitCode)) {
		t.Fatal("expected unsigned winget no-match exit code to be recognized")
	}
	if !isWingetNoApplicationsFoundExitCode(-1978335212) {
		t.Fatal("expected signed winget no-match exit code to be recognized")
	}
	if isWingetNoApplicationsFoundExitCode(1) {
		t.Fatal("did not expect generic exit code to be treated as winget no-match")
	}
}

func TestBuildUpdateCheckResultUsesWingetChannelTruth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceWinget,
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	originalStatus := queryWingetPackageStatusForChannels
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		queryWingetPackageStatusForChannels = originalStatus
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }
	queryWingetPackageStatusForChannels = func() (wingetPackageStatus, error) {
		return wingetPackageStatus{
			Installed:        true,
			UpgradeAvailable: true,
			InstalledVersion: "0.3.0",
			AvailableVersion: "0.4.0",
			InventoryScope:   "published_source",
		}, nil
	}
	wingetLink := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(wingetLink), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(wingetLink, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "update_available" {
		t.Fatalf("result.Status = %q, want update_available", result.Status)
	}
	if result.Source != "winget" {
		t.Fatalf("result.Source = %q, want winget", result.Source)
	}
	if result.InstallSource != installSourceWinget {
		t.Fatalf("result.InstallSource = %q, want %q", result.InstallSource, installSourceWinget)
	}
	if result.CacheStatus != "not_used" {
		t.Fatalf("result.CacheStatus = %q, want not_used", result.CacheStatus)
	}
	if result.CurrentVersion != "0.3.0" {
		t.Fatalf("result.CurrentVersion = %q, want 0.3.0", result.CurrentVersion)
	}
	if result.LatestVersion != "0.4.0" {
		t.Fatalf("result.LatestVersion = %q, want 0.4.0", result.LatestVersion)
	}
	if !strings.Contains(result.Message, "Update available via winget") {
		t.Fatalf("expected winget update message, got %q", result.Message)
	}
}

func TestBuildUpdateCheckResultHandlesLocalWingetManifestInventory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := saveState(paths, installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceWinget,
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	originalStatus := queryWingetPackageStatusForChannels
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		queryWingetPackageStatusForChannels = originalStatus
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }
	queryWingetPackageStatusForChannels = func() (wingetPackageStatus, error) {
		return wingetPackageStatus{
			Installed:        true,
			InstalledVersion: "0.3.0",
			InventoryScope:   "local_manifest",
		}, nil
	}
	wingetLink := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(wingetLink), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(wingetLink, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	result := buildUpdateCheckResult(paths)
	if result.Status != "local_manifest" {
		t.Fatalf("result.Status = %q, want local_manifest", result.Status)
	}
	if result.Source != "winget_local_manifest" {
		t.Fatalf("result.Source = %q, want winget_local_manifest", result.Source)
	}
	if !strings.Contains(result.Message, "Installed via local winget manifest") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}

func TestBuildUpdateCheckResultFlagsMixedWindowsChannels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.StateFile), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	state := installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceBundle,
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	bundleRoot := windowsBundleInstallRoot(home)
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("mkdir bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "bundle.json"), []byte(`{"version":"0.3.0"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}

	wingetLink := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(wingetLink), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(wingetLink, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }

	result := buildUpdateCheckResult(paths)
	if result.Status != "channel_conflict" {
		t.Fatalf("result.Status = %q, want channel_conflict", result.Status)
	}
	if result.Source != "mixed_channels" {
		t.Fatalf("result.Source = %q, want mixed_channels", result.Source)
	}
	if !strings.Contains(result.Message, "both bundle and winget installs are present") {
		t.Fatalf("expected conflict message, got %q", result.Message)
	}
}

func TestBuildUpdateCheckResultIgnoresStaleWingetPackageRemnants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	state := installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceBundle,
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	bundleRoot := windowsBundleInstallRoot(home)
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("mkdir bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "bundle.json"), []byte(`{"version":"0.3.0"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}

	staleRoot := filepath.Join(windowsWingetPackageRoot(home), wingetPackageID+"_0.3.0_x64", "ha-nova")
	if err := os.MkdirAll(staleRoot, 0o755); err != nil {
		t.Fatalf("mkdir stale winget root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleRoot, publicBinaryName()), []byte("stale"), 0o755); err != nil {
		t.Fatalf("write stale winget binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleRoot, "bundle.json"), []byte(`{"version":"0.2.0"}`), 0o644); err != nil {
		t.Fatalf("write stale winget metadata: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	originalStatus := queryWingetPackageStatusForChannels
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
		queryWingetPackageStatusForChannels = originalStatus
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }
	queryWingetPackageStatusForChannels = func() (wingetPackageStatus, error) {
		return wingetPackageStatus{}, nil
	}

	result := buildUpdateCheckResult(paths)
	if result.Status == "channel_conflict" {
		t.Fatalf("expected stale winget remnants to be ignored, got %q", result.Message)
	}
	if result.Source != "github_releases" {
		t.Fatalf("result.Source = %q, want github_releases", result.Source)
	}
}

func TestResolveWingetBundleRootUsesSingleLiveCandidate(t *testing.T) {
	home := t.TempDir()
	linkPath := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(linkPath, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	want := filepath.Join(windowsWingetPackageRoot(home), wingetPackageID+"_0.4.0_x64", "ha-nova")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("mkdir winget bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, publicBinaryName()), []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, "bundle.json"), []byte(`{"version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write winget metadata: %v", err)
	}

	got := resolveWingetBundleRoot(home)
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("resolveWingetBundleRoot() = %q, want %q", got, want)
	}
}

func TestWingetInstallPresentOnDiskUsesInventoryWhenLinkMissing(t *testing.T) {
	home := t.TempDir()

	want := filepath.Join(windowsWingetPackageRoot(home), wingetPackageID+"_0.4.0_x64", "ha-nova")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("mkdir winget bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, publicBinaryName()), []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, "bundle.json"), []byte(`{"version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write winget metadata: %v", err)
	}

	originalStatus := queryWingetPackageStatusForChannels
	defer func() {
		queryWingetPackageStatusForChannels = originalStatus
	}()
	queryWingetPackageStatusForChannels = func() (wingetPackageStatus, error) {
		return wingetPackageStatus{Installed: true}, nil
	}

	if !wingetInstallPresentOnDisk(home) {
		t.Fatal("expected winget install to be detected from inventory when link is missing")
	}
}

func TestResolveWingetBundleRootFallsBackToSingleCandidateWhenLinkMissing(t *testing.T) {
	home := t.TempDir()

	want := filepath.Join(windowsWingetPackageRoot(home), wingetPackageID+"_0.4.0_x64", "ha-nova")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("mkdir winget bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, publicBinaryName()), []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(want, "bundle.json"), []byte(`{"version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write winget metadata: %v", err)
	}

	got := resolveWingetBundleRoot(home)
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("resolveWingetBundleRoot() = %q, want %q", got, want)
	}
}

func TestRunUpdateFailsLoudOnMixedWindowsChannels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	state := installState{
		SchemaVersion: stateSchemaVersion,
		InstallSource: installSourceBundle,
	}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	bundleRoot := windowsBundleInstallRoot(home)
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("mkdir bundle root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, "bundle.json"), []byte(`{"version":"0.3.0"}`), 0o644); err != nil {
		t.Fatalf("write bundle metadata: %v", err)
	}

	wingetLink := windowsWingetLinkPath(home)
	if err := os.MkdirAll(filepath.Dir(wingetLink), 0o755); err != nil {
		t.Fatalf("mkdir winget link dir: %v", err)
	}
	if err := os.WriteFile(wingetLink, []byte("winget"), 0o755); err != nil {
		t.Fatalf("write winget link: %v", err)
	}

	originalPlatform := channelChecksUseWindowsPlatform
	defer func() {
		channelChecksUseWindowsPlatform = originalPlatform
	}()
	channelChecksUseWindowsPlatform = func() bool { return true }

	exitCode, output := captureCommandOutput(t, func() int {
		return runUpdate(paths, nil)
	})
	if exitCode != 1 {
		t.Fatalf("runUpdate() exit = %d, want 1\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Windows install channel conflict") {
		t.Fatalf("expected channel conflict output, got:\n%s", output)
	}
}
