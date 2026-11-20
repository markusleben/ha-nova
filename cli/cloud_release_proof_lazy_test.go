package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CLI-01/PERF-05: the digest check waits for the first question whether Link
// may run and is made once: bytes changed after the start are refused on
// first use, and putting them back doesn't change that answer.
func TestCloudReleaseProofRunsOnceOnFirstUse(t *testing.T) {
	if !cloudRemotePlatformSupported(runtime.GOOS) {
		t.Skip("no Link on this platform")
	}
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()
	paths := writeCloudFeatureReleaseBundle(t, "2.0.0", "2.0.0", enabledCloudFeatureMetadata("2.0.0"))
	configureCloudRemoteFeature(paths)

	path := filepath.Join(paths.InstallRoot, publicBinaryName())
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if cloudRemoteReleaseVerified() {
		t.Fatal("bytes changed after the start passed: the digest was checked before first use")
	}
	if err := os.WriteFile(path, original, 0o700); err != nil {
		t.Fatal(err)
	}
	if cloudRemoteReleaseVerified() || cloudRemoteFeatureAvailable() {
		t.Fatal("the first answer wasn't kept for the process")
	}
}

// The check reads the executable this process started from: a file put at
// its path after the start changes neither answer.
func TestCloudReleaseProofReadsTheExecutableItStartedFrom(t *testing.T) {
	if runtime.GOOS == "windows" || !cloudRemotePlatformSupported(runtime.GOOS) {
		t.Skip("Windows doesn't replace a running executable")
	}
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()
	paths := writeCloudFeatureReleaseBundle(t, "2.0.0", "2.0.0", enabledCloudFeatureMetadata("2.0.0"))
	configureCloudRemoteFeature(paths)

	path := filepath.Join(paths.InstallRoot, publicBinaryName())
	if err := os.WriteFile(path+".new", []byte("tampered runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if !cloudRemoteFeatureAvailable() || !cloudRemoteReleaseVerified() {
		t.Fatal("the release this process started from was refused after its path changed")
	}

	configureCloudRemoteFeature(paths) // a process started from the tampered file
	if cloudRemoteFeatureAvailable() || cloudRemoteReleaseVerified() {
		t.Fatal("a tampered executable passed")
	}
}
